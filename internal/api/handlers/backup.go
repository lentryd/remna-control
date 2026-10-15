package handlers

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/gofiber/fiber/v2"

	"remna-control/internal/apperr"
	"remna-control/internal/audit"
	"remna-control/internal/backup"
)

// maxBackupSize caps uploaded archives (also the app's body limit).
const maxBackupSize = 256 << 20

func (h *Handlers) BackupCategories(c *fiber.Ctx) error {
	counts, err := backup.Counts(c.UserContext(), h.SQL)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"categories": backup.Categories, "counts": counts})
}

// ExportBackup streams a zip of the categories in ?categories=a,b.
func (h *Handlers) ExportBackup(c *fiber.Ctx) error {
	cats := splitList(c.Query("categories"))
	var buf bytes.Buffer
	m, err := backup.Export(c.UserContext(), h.SQL, &buf, cats, h.Version)
	audit.Log(c.UserContext(), h.DB, "backup.export", "", 0, cats, err)
	if err != nil {
		return badRequest(err)
	}
	name := "vpn-control-backup-" + m.CreatedAt.Format("20060102-150405") + ".zip"
	c.Set(fiber.HeaderContentType, "application/zip")
	c.Set(fiber.HeaderContentDisposition, `attachment; filename="`+name+`"`)
	return c.Send(buf.Bytes())
}

// InspectBackup reads an uploaded archive's manifest so the UI can show
// what's inside before importing.
func (h *Handlers) InspectBackup(c *fiber.Ctx) error {
	a, err := uploadedBackup(c)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"manifest": a.Manifest, "available": a.Available()})
}

// ImportBackup replaces the selected categories with the archive's data.
// The current state of those categories is saved next to the database
// first, so a wrong file can be rolled back.
func (h *Handlers) ImportBackup(c *fiber.Ctx) error {
	a, err := uploadedBackup(c)
	if err != nil {
		return err
	}
	cats := splitList(c.FormValue("categories"))
	if err := backup.Validate(cats); err != nil {
		return badRequest(err)
	}
	return h.restore(c, a, cats)
}

// restore imports cats from a after saving their current state as a
// pre-import snapshot, so a wrong file can be rolled back.
func (h *Handlers) restore(c *fiber.Ctx, a *backup.Archive, cats []string) error {
	ctx := c.UserContext()
	snapshot, err := h.Snapshots.Create(ctx, backup.KindPreImport, cats)
	if err != nil {
		return badRequest(apperr.Wrap(err, "backup.snapshot_failed", "couldn't save the current data before importing: {{error}}"))
	}
	rep, err := backup.Import(ctx, h.SQL, a, cats)
	audit.Log(ctx, h.DB, "backup.import", "", 0, fiber.Map{
		"categories": cats, "from": a.Manifest.CreatedAt, "app_version": a.Manifest.AppVersion, "snapshot": snapshot.Name,
	}, err)
	if err != nil {
		var ie *backup.IntegrityError
		if errors.As(err, &ie) {
			return apperr.Status(fiber.StatusConflict, "backup.integrity", "import cancelled: rows reference missing data ({{details}}); import them together with the categories they depend on", "details", strings.Join(ie.Details, "; "))
		}
		return badRequest(err)
	}
	return c.JSON(fiber.Map{"report": rep, "snapshot": snapshot.Name})
}

// ListSnapshots: GET /backup/snapshots (and /v1/backups).
func (h *Handlers) ListSnapshots(c *fiber.Ctx) error {
	list, err := h.Snapshots.List()
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"snapshots": list})
}

// CreateSnapshot: POST /backup/snapshots — a full manual snapshot.
func (h *Handlers) CreateSnapshot(c *fiber.Ctx) error {
	sn, err := h.Snapshots.Create(c.UserContext(), backup.KindManual, nil)
	audit.Log(c.UserContext(), h.DB, "backup.snapshot", "", 0, sn, err)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(sn)
}

// CreateAndSendSnapshot: POST /v1/backups — takes a manual snapshot and
// returns the archive in the response.
func (h *Handlers) CreateAndSendSnapshot(c *fiber.Ctx) error {
	sn, err := h.Snapshots.Create(c.UserContext(), backup.KindManual, nil)
	audit.Log(c.UserContext(), h.DB, "backup.snapshot", "", 0, sn, err)
	if err != nil {
		return err
	}
	return h.sendSnapshot(c, sn.Name)
}

// DownloadSnapshot: GET /backup/snapshots/:name (and /v1/backups/:name).
func (h *Handlers) DownloadSnapshot(c *fiber.Ctx) error {
	return h.sendSnapshot(c, c.Params("name"))
}

func (h *Handlers) sendSnapshot(c *fiber.Ctx, name string) error {
	p, err := h.snapshotPath(name)
	if err != nil {
		return err
	}
	c.Set(fiber.HeaderContentType, "application/zip")
	c.Set(fiber.HeaderContentDisposition, `attachment; filename="vpn-control-`+name+`"`)
	return c.SendFile(p)
}

func (h *Handlers) DeleteSnapshot(c *fiber.Ctx) error {
	name := c.Params("name")
	if _, err := h.snapshotPath(name); err != nil {
		return err
	}
	err := h.Snapshots.Delete(name)
	audit.Log(c.UserContext(), h.DB, "backup.snapshot_delete", "", 0, name, err)
	if err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// InspectSnapshot: GET /backup/snapshots/:name/inspect — like InspectBackup.
func (h *Handlers) InspectSnapshot(c *fiber.Ctx) error {
	a, err := h.openSnapshot(c.Params("name"))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"manifest": a.Manifest, "available": a.Available()})
}

// RestoreSnapshot: POST /backup/snapshots/:name/restore {categories}.
func (h *Handlers) RestoreSnapshot(c *fiber.Ctx) error {
	var in struct {
		Categories []string `json:"categories"`
	}
	if err := bind(c, &in); err != nil {
		return err
	}
	if err := backup.Validate(in.Categories); err != nil {
		return badRequest(err)
	}
	a, err := h.openSnapshot(c.Params("name"))
	if err != nil {
		return err
	}
	return h.restore(c, a, in.Categories)
}

func (h *Handlers) snapshotPath(name string) (string, error) {
	p, err := h.Snapshots.Path(name)
	switch {
	case errors.Is(err, backup.ErrBadName):
		return "", apperr.New("backup.bad_name", "invalid snapshot name")
	case err != nil:
		return "", apperr.Status(fiber.StatusNotFound, "backup.not_found", "snapshot not found")
	}
	return p, nil
}

func (h *Handlers) openSnapshot(name string) (*backup.Archive, error) {
	p, err := h.snapshotPath(name)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	a, err := backup.Open(data)
	if err != nil {
		return nil, badRequest(err)
	}
	return a, nil
}

func uploadedBackup(c *fiber.Ctx) (*backup.Archive, error) {
	fh, err := c.FormFile("file")
	if err != nil {
		return nil, apperr.New("backup.no_file", "no file uploaded")
	}
	if fh.Size > maxBackupSize {
		return nil, apperr.Status(fiber.StatusRequestEntityTooLarge, "backup.too_large", "file is too large")
	}
	f, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	a, err := backup.Open(data)
	if err != nil {
		return nil, badRequest(err)
	}
	return a, nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
