package badger_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/rebac/data"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/rebac/data/badger"
)

func TestBadgerGetUsers(t *testing.T) {
	tempdir := t.TempDir()

	db, err := badger.New(tempdir)
	if err != nil {
		t.Fatalf("failed to create badger db: %v", err)
	}
	defer db.Close()

	user := data.User{
		ID:    "test",
		Alias: []string{"test"},
	}

	if err := db.CreateUser(user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	user2 := data.User{
		ID:    "test2",
		Alias: []string{"test2"},
	}

	if err := db.CreateUser(user2); err != nil {
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
	tempdir := t.TempDir()

	db, err := badger.New(tempdir)
	if err != nil {
		t.Fatalf("failed to create badger db: %v", err)
	}
	defer db.Close()

	user := data.User{
		Alias: []string{"test", "test2"},
	}

	if err := db.CreateUser(user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	user = data.User{
		Alias: []string{"test2", "test3"},
	}

	err = db.CreateUser(user)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	if !errors.Is(err, data.ErrConflict) {
		t.Fatalf("failed to create user: %v", err)
	}

	res, err := db.GetUsers(data.GetUserRequest{
		Alias: "test2",
	})
	if err != nil {
		t.Fatalf("failed to get users: %v", err)
	}

	err = db.DeleteUser(res.Payload[0].ID)
	if err != nil {
		t.Fatalf("failed to delete user: %v", err)
	}
}

func TestBadgerPutUser(t *testing.T) {
	tempdir := t.TempDir()

	db, err := badger.New(tempdir)
	if err != nil {
		t.Fatalf("failed to create badger db: %v", err)
	}
	defer db.Close()

	user := data.User{
		Alias: []string{"test"},
	}

	if err := db.CreateUser(user); err != nil {
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

	if err := db.PutUser(user); err != nil {
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
	tempdir := t.TempDir()

	db, err := badger.New(tempdir)
	if err != nil {
		t.Fatalf("failed to create badger db: %v", err)
	}
	defer db.Close()

	user := data.User{
		Alias: []string{"test"},
		Roles: []string{"role-1"},
	}

	if err := db.CreateUser(user); err != nil {
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
	user.Roles = []string{"role-2"}

	if err := db.PatchUser(user); err != nil {
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

	if slices.Compare(res.Payload[0].Alias, []string{"test", "test2"}) != 0 {
		t.Fatalf("expected alias test2, got %v", res.Payload[0].Alias)
	}

	if slices.Compare(res.Payload[0].Roles, []string{"role-1", "role-2"}) != 0 {
		t.Fatalf("expected role-1, got %v", res.Payload[0].Roles)
	}
}
