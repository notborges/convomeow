package whatsapp

import (
	"context"
	"path/filepath"
	"testing"

	appsqlite "github.com/notborges/convomeow/internal/store/sqlite"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waAdv"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
)

func TestContactsMergePhoneAndLIDAliases(t *testing.T) {
	ctx := context.Background()
	container, err := sqlstore.New(ctx, "sqlite3", appsqlite.DSN(filepath.Join(t.TempDir(), "whatsmeow.sqlite")), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer container.Close()
	account := types.NewJID("15550000000", types.DefaultUserServer)
	pn := types.NewJID("15551234567", types.DefaultUserServer)
	lid := types.NewJID("abc123", types.HiddenUserServer)
	device := container.NewDevice()
	device.ID = &account
	device.Account = &waAdv.ADVSignedDeviceIdentity{Details: []byte{1}, AccountSignature: make([]byte, 64),
		AccountSignatureKey: make([]byte, 32), DeviceSignature: make([]byte, 64)}
	if err := device.Save(ctx); err != nil {
		t.Fatal(err)
	}
	contacts := device.Contacts
	if err := contacts.PutContactName(ctx, pn, "Alice", "Alice Smith"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := contacts.PutPushName(ctx, lid, "A. Smith"); err != nil {
		t.Fatal(err)
	}
	if err := device.LIDs.PutLIDMapping(ctx, lid, pn); err != nil {
		t.Fatal(err)
	}
	session := &session{client: whatsmeow.NewClient(device, nil)}
	list, err := session.Contacts(ctx)
	if err != nil || len(list) != 1 || list[0].Name != "Alice Smith" || list[0].Phone != "+15551234567" || list[0].AlternateID != lid.String() {
		t.Fatalf("merged contacts: %+v, %v", list, err)
	}
	contact, err := session.Contact(ctx, lid.String())
	if err != nil || contact.ProviderID != pn.String() || contact.Name != "Alice Smith" || contact.AlternateID != lid.String() {
		t.Fatalf("contact by LID: %+v, %v", contact, err)
	}
}

func TestAvatarURLRejectsUnknownHosts(t *testing.T) {
	for _, raw := range []string{"http://pps.whatsapp.net/photo", "https://127.0.0.1/photo", "https://whatsapp.net.evil.test/photo", "https://user@pps.whatsapp.net/photo"} {
		if checkAvatarURL(raw) == nil {
			t.Fatalf("accepted avatar URL %q", raw)
		}
	}
	if err := checkAvatarURL("https://pps.whatsapp.net/photo"); err != nil {
		t.Fatal(err)
	}
}
