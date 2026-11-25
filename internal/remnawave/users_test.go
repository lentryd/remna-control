package remnawave

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUpdateUserRequestMarshalExternalSquad(t *testing.T) {
	uuid := "u1"
	cases := []struct {
		name string
		req  UpdateUserRequest
		want string // substring; "" means the key must be absent
	}{
		{"set", UpdateUserRequest{ID: 1, ExternalSquadUUID: &uuid}, `"externalSquadUuid":"u1"`},
		{"clear", UpdateUserRequest{ID: 1, ClearExternalSquad: true}, `"externalSquadUuid":null`},
		{"unset", UpdateUserRequest{ID: 1}, ""},
	}
	for _, c := range cases {
		b, err := json.Marshal(c.req)
		if err != nil {
			t.Fatal(err)
		}
		got := string(b)
		if c.want == "" {
			if strings.Contains(got, "externalSquadUuid") {
				t.Errorf("%s: unexpected key in %s", c.name, got)
			}
		} else if !strings.Contains(got, c.want) {
			t.Errorf("%s: %s lacks %s", c.name, got, c.want)
		}
	}
}

func TestUpdateUserRequestMarshalClearBoth(t *testing.T) {
	b, err := json.Marshal(UpdateUserRequest{ID: 1, ClearTelegramID: true, ClearExternalSquad: true})
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.Contains(got, `"telegramId":null`) || !strings.Contains(got, `"externalSquadUuid":null`) {
		t.Errorf("both nulls expected, got %s", got)
	}
}
