package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/notborges/convomeow/internal/core"
)

func TestAvatarRecordsAreAccountScopedAndReplacementsAreCleaned(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "app.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	for _, id := range []string{"first", "second"} {
		if err := store.CreateAccount(ctx, core.Account{ID: id, Provider: core.ProviderWhatsApp,
			ConnectionKind: core.ConnectionKindLinkedDevice, Label: id, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	first := core.AvatarRecord{AccountID: "first", ProviderID: "111@s.whatsapp.net", PictureID: "v1",
		ProfileID: "local", ObjectKey: "first-object", ContentType: "image/png", Size: 5, CheckedAt: now}
	second := first
	second.AccountID, second.ObjectKey = "second", "second-object"
	for _, record := range []core.AvatarRecord{first, second} {
		if err := store.SaveAvatar(ctx, record); err != nil {
			t.Fatal(err)
		}
	}
	first.PictureID, first.ObjectKey = "v2", "new-first-object"
	if err := store.SaveAvatar(ctx, first); err != nil {
		t.Fatal(err)
	}
	used, err := store.StoredMediaBytes(ctx)
	if err != nil || used != 10 {
		t.Fatalf("stored avatar bytes: %d, %v", used, err)
	}
	if err := store.ClearAccountAvatars(ctx, "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetAvatar(ctx, "first", first.ProviderID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("first account avatar remains: %v", err)
	}
	kept, err := store.GetAvatar(ctx, "second", second.ProviderID)
	if err != nil || kept.ObjectKey != second.ObjectKey {
		t.Fatalf("second account avatar lost: %+v, %v", kept, err)
	}
	orphans, err := store.ListMediaOrphans(ctx, 10)
	if err != nil || len(orphans) != 2 || orphans[0].Key != "first-object" || orphans[1].Key != "new-first-object" {
		t.Fatalf("orphaned avatars: %+v, %v", orphans, err)
	}
}
