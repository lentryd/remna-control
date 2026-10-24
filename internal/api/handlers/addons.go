package handlers

import (
	"strings"

	"github.com/gofiber/fiber/v2"

	"remna-control/ent"
	"remna-control/ent/addon"
	"remna-control/ent/subscriptionaddon"
	"remna-control/ent/tariff"
	"remna-control/internal/apperr"
	"remna-control/internal/audit"
)

type catalogAddonView struct {
	ID              int               `json:"id"`
	Name            string            `json:"name"`
	Prefix          string            `json:"prefix"`
	Suffix          string            `json:"suffix"`
	InConfig        bool              `json:"in_config"`
	Source          string            `json:"source"`
	Archived        bool              `json:"archived"`
	Remark          string            `json:"remark"`
	RemarkUnlimited string            `json:"remark_unlimited"`
	Stubs           map[string]string `json:"stubs"`
}

func (h *Handlers) ListAddons(c *fiber.Ctx) error {
	list, err := h.DB.Addon.Query().Order(ent.Asc(addon.FieldName)).All(c.UserContext())
	if err != nil {
		return err
	}
	out := make([]catalogAddonView, 0, len(list))
	for _, a := range list {
		stubs := a.Stubs
		if stubs == nil {
			stubs = map[string]string{}
		}
		out = append(out, catalogAddonView{
			ID: a.ID, Name: a.Name, Prefix: a.Prefix, Suffix: a.Suffix, InConfig: a.InConfig, Source: a.Source.String(), Archived: a.Archived,
			Remark: a.Remark, RemarkUnlimited: a.RemarkUnlimited, Stubs: stubs,
		})
	}
	return c.JSON(out)
}

type addonCatalogInput struct {
	Name            string            `json:"name"`
	Prefix          string            `json:"prefix"`
	Suffix          string            `json:"suffix"`
	Remark          string            `json:"remark"`
	RemarkUnlimited string            `json:"remark_unlimited"`
	Stubs           map[string]string `json:"stubs"`
}

func (in *addonCatalogInput) validate() error {
	in.Name, in.Prefix, in.Suffix = strings.TrimSpace(in.Name), strings.TrimSpace(in.Prefix), strings.TrimSpace(in.Suffix)
	if in.Prefix == "" && in.Suffix == "" {
		return apperr.New("addon.prefix_required", "set a prefix or a suffix")
	}
	if in.Name == "" {
		in.Name = in.Prefix + in.Suffix
	}
	for k, v := range in.Stubs {
		if strings.TrimSpace(v) == "" {
			delete(in.Stubs, k)
		}
	}
	return nil
}

// uiAddon loads an add-on that may be edited here (not one from the file).
func (h *Handlers) uiAddon(c *fiber.Ctx) (*ent.Addon, error) {
	id, err := paramID(c, "id")
	if err != nil {
		return nil, err
	}
	a, err := h.DB.Addon.Get(c.UserContext(), id)
	if err != nil {
		return nil, badRequest(err)
	}
	if a.Source != addon.SourceUI {
		return nil, apperr.Status(fiber.StatusConflict, "addon.from_file", "the add-on comes from the file; edit it there")
	}
	return a, nil
}

func (h *Handlers) CreateAddon(c *fiber.Ctx) error {
	var in addonCatalogInput
	if err := bind(c, &in); err != nil {
		return err
	}
	if err := in.validate(); err != nil {
		return err
	}
	ctx := c.UserContext()
	a, err := h.DB.Addon.Create().
		SetName(in.Name).SetPrefix(in.Prefix).SetSuffix(in.Suffix).SetSource(addon.SourceUI).
		SetRemark(in.Remark).SetRemarkUnlimited(in.RemarkUnlimited).SetStubs(in.Stubs).
		Save(ctx)
	if ent.IsConstraintError(err) {
		err = apperr.Status(fiber.StatusConflict, "addon.duplicate", "an add-on with this name already exists")
	}
	audit.Log(ctx, h.DB, "addon.create", "addon", idOfAddon(a), in, err)
	if err != nil {
		return badRequest(err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": a.ID})
}

func (h *Handlers) UpdateAddon(c *fiber.Ctx) error {
	a, err := h.uiAddon(c)
	if err != nil {
		return err
	}
	var in addonCatalogInput
	if err := bind(c, &in); err != nil {
		return err
	}
	if err := in.validate(); err != nil {
		return err
	}
	ctx := c.UserContext()
	err = h.DB.Addon.UpdateOne(a).
		SetName(in.Name).SetPrefix(in.Prefix).SetSuffix(in.Suffix).
		SetRemark(in.Remark).SetRemarkUnlimited(in.RemarkUnlimited).SetStubs(in.Stubs).
		Exec(ctx)
	if ent.IsConstraintError(err) {
		err = apperr.Status(fiber.StatusConflict, "addon.duplicate", "an add-on with this name already exists")
	}
	audit.Log(ctx, h.DB, "addon.update_catalog", "addon", a.ID, in, err)
	if err != nil {
		return badRequest(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// SetAddonArchived: POST /addons/:id/archive and /restore.
func (h *Handlers) SetAddonArchived(archived bool) fiber.Handler {
	return func(c *fiber.Ctx) error {
		a, err := h.uiAddon(c)
		if err != nil {
			return err
		}
		err = h.DB.Addon.UpdateOne(a).SetArchived(archived).Exec(c.UserContext())
		audit.Log(c.UserContext(), h.DB, "addon.archive", "addon", a.ID, fiber.Map{"archived": archived}, err)
		if err != nil {
			return badRequest(err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	}
}

func (h *Handlers) DeleteAddon(c *fiber.Ctx) error {
	a, err := h.uiAddon(c)
	if err != nil {
		return err
	}
	ctx := c.UserContext()
	if !a.Archived {
		return apperr.Status(fiber.StatusConflict, "addon.not_archived", "archive the add-on first; only archived add-ons can be deleted")
	}
	used, err := h.DB.SubscriptionAddon.Query().Where(subscriptionaddon.AddonID(a.ID)).Exist(ctx)
	if err != nil {
		return err
	}
	tariffs, err := h.DB.Tariff.Query().Where(tariff.AddonID(a.ID)).Exist(ctx)
	if err != nil {
		return err
	}
	if used || tariffs {
		return apperr.Status(fiber.StatusConflict, "addon.in_use", "the add-on is used by subscriptions or tariffs")
	}
	err = h.DB.Addon.DeleteOne(a).Exec(ctx)
	audit.Log(ctx, h.DB, "addon.delete", "addon", a.ID, nil, err)
	if err != nil {
		return badRequest(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func idOfAddon(a *ent.Addon) int {
	if a == nil {
		return 0
	}
	return a.ID
}
