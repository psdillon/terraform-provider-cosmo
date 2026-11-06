package api_key

import (
	"testing"

	platformv1 "github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/platform/v1"
	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/api"
)

func TestInferExpiry(t *testing.T) {
	created := "2026-01-15T10:00:00.000Z"
	cases := map[string]string{
		"":                         api.ApiKeyExpiresNever,
		"2026-02-14T10:00:00.000Z": api.ApiKeyExpiresThirtyDays,
		"2026-07-15T10:00:00.000Z": api.ApiKeyExpiresSixMonths,
		"2027-01-15T10:00:00.000Z": api.ApiKeyExpiresOneYear,
	}

	for expiresAt, want := range cases {
		got := inferExpiry(&platformv1.APIKey{CreatedAt: created, ExpiresAt: expiresAt})
		if got != want {
			t.Errorf("inferExpiry(expiresAt=%q) = %q, want %q", expiresAt, got, want)
		}
	}
}
