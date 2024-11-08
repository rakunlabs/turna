package badger_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data/badger"
)

func TestBadgerGetUsers(t *testing.T) {
	// tempdir := t.TempDir()

	db, err := badger.New("", true, false)
	// db, err := badger.New(tempdir, true)
	if err != nil {
		t.Fatalf("failed to create badger db: %v", err)
	}
	defer db.Close()

	user := data.User{
		Alias: []string{"test"},
	}

	ctx := data.WithContextUserName(context.Background(), "testing")

	if _, err := db.CreateUser(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	user2 := data.User{
		Alias: []string{"test2"},
	}

	if _, err := db.CreateUser(ctx, user2); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	req := data.GetUserRequest{
		Alias: "test",
	}

	res, err := db.GetUsers(req)
	if err != nil {
		t.Fatalf("failed to get users: %v", err)
	}

	if len(res.Payload) != 1 {
		t.Fatalf("expected 1 user, got %d", len(res.Payload))
	}

	req = data.GetUserRequest{
		ID: res.Payload[0].ID,
	}

	res, err = db.GetUsers(req)
	if err != nil {
		t.Fatalf("failed to get users: %v", err)
	}

	if len(res.Payload) != 1 {
		t.Fatalf("expected 1 user, got %d", len(res.Payload))
	}

	req = data.GetUserRequest{}

	res, err = db.GetUsers(req)
	if err != nil {
		t.Fatalf("failed to get users: %v", err)
	}

	if len(res.Payload) != 2 {
		t.Fatalf("expected 2 users, got %d", len(res.Payload))
	}

	if res.Meta.TotalItemCount != 2 {
		t.Fatalf("expected total item count 2, got %d", res.Meta.TotalItemCount)
	}
}

func TestBadgerCreateUser(t *testing.T) {
	// tempdir := t.TempDir()

	db, err := badger.New("", true, false)
	if err != nil {
		t.Fatalf("failed to create badger db: %v", err)
	}
	defer db.Close()

	user := data.User{
		Alias: []string{"test", "test2"},
	}

	ctx := data.WithContextUserName(context.Background(), "testing")

	if _, err := db.CreateUser(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	user = data.User{
		Alias: []string{"test2", "test3"},
	}

	_, err = db.CreateUser(ctx, user)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	if !errors.Is(err, data.ErrConflict) {
		t.Fatalf("failed to create user error miss match: %v", err)
	}

	res, err := db.GetUsers(data.GetUserRequest{
		Alias: "test2",
	})
	if err != nil {
		t.Fatalf("failed to get users: %v", err)
	}

	err = db.DeleteUser(ctx, res.Payload[0].ID)
	if err != nil {
		t.Fatalf("failed to delete user: %v", err)
	}
}

func TestBadgerPutUser(t *testing.T) {
	// tempdir := t.TempDir()

	db, err := badger.New("", true, false)
	if err != nil {
		t.Fatalf("failed to create badger db: %v", err)
	}
	defer db.Close()

	user := data.User{
		Alias: []string{"test"},
	}

	ctx := data.WithContextUserName(context.Background(), "testing")

	if _, err := db.CreateUser(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	req := data.GetUserRequest{
		Alias: "test",
	}

	res, err := db.GetUsers(req)
	if err != nil {
		t.Fatalf("failed to get users: %v", err)
	}

	user.Alias = []string{"test2"}
	user.ID = res.Payload[0].ID

	if err := db.PutUser(ctx, user); err != nil {
		t.Fatalf("failed to update user: %v", err)
	}

	req = data.GetUserRequest{
		Alias: "test2",
	}

	res, err = db.GetUsers(req)
	if err != nil {
		t.Fatalf("failed to get users: %v", err)
	}

	if len(res.Payload) != 1 {
		t.Fatalf("expected 1 user, got %d", len(res.Payload))
	}

	if slices.Compare(res.Payload[0].Alias, []string{"test2"}) != 0 {
		t.Fatalf("expected alias test2, got %v", res.Payload[0].Alias)
	}
}

func TestBadgerPatchUser(t *testing.T) {
	// tempdir := t.TempDir()

	db, err := badger.New("", true, false)
	if err != nil {
		t.Fatalf("failed to create badger db: %v", err)
	}
	defer db.Close()

	user := data.User{
		Alias:   []string{"test"},
		RoleIDs: []string{"role-1"},
	}

	ctx := data.WithContextUserName(context.Background(), "testing")

	if _, err := db.CreateUser(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	req := data.GetUserRequest{
		Alias: "test",
	}

	res, err := db.GetUsers(req)
	if err != nil {
		t.Fatalf("failed to get users: %v", err)
	}

	userPath := data.UserPatch{
		Alias:   &[]string{"test2"},
		RoleIDs: &[]string{},
	}

	if err := db.PatchUser(ctx, res.Payload[0].ID, userPath); err != nil {
		t.Fatalf("failed to update user: %v", err)
	}

	req = data.GetUserRequest{
		Alias: "test2",
	}

	res, err = db.GetUsers(req)
	if err != nil {
		t.Fatalf("failed to get users: %v", err)
	}

	if len(res.Payload) != 1 {
		t.Fatalf("expected 1 user, got %d", len(res.Payload))
	}

	if slices.Compare(res.Payload[0].Alias, []string{"test2"}) != 0 {
		t.Fatalf("expected alias test2, got %v", res.Payload[0].Alias)
	}

	if slices.Compare(res.Payload[0].RoleIDs, []string{}) != 0 {
		t.Fatalf("expected role-1, got %v", res.Payload[0].RoleIDs)
	}
}
