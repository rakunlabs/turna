package auth

import (
	"net/http/httptest"
	"testing"
)

func TestParseListQueryPagination(t *testing.T) {
	tests := []struct {
		name       string
		rawQuery   string
		wantLimit  int64
		wantOffset int64
		wantError  bool
	}{
		{name: "defaults", wantLimit: 20},
		{name: "canonical", rawQuery: "_limit=50&_offset=10", wantLimit: 50, wantOffset: 10},
		{name: "legacy aliases", rawQuery: "limit=40&offset=5", wantLimit: 40, wantOffset: 5},
		{name: "canonical wins", rawQuery: "limit=40&_limit=30&offset=5&_offset=3", wantLimit: 30, wantOffset: 3},
		{name: "explicit unlimited", rawQuery: "limit=40&_limit=0", wantLimit: 0},
		{name: "invalid limit", rawQuery: "_limit=invalid", wantError: true},
		{name: "invalid offset", rawQuery: "offset=-1", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/?"+tt.rawQuery, nil)
			q, err := parseListQuery(r)
			if (err != nil) != tt.wantError {
				t.Fatalf("parseListQuery() error = %v, wantError %v", err, tt.wantError)
			}
			if err != nil {
				return
			}

			limit, offset := getLimitOffset(q)
			if limit != tt.wantLimit || offset != tt.wantOffset {
				t.Fatalf("pagination = (%d, %d), want (%d, %d)", limit, offset, tt.wantLimit, tt.wantOffset)
			}
		})
	}
}

func TestParseUserQueryNormalizesRoleType(t *testing.T) {
	r := httptest.NewRequest("GET", "/?role_type=%20temporary%20", nil)
	req, err := parseUserQuery(r)
	if err != nil {
		t.Fatalf("parseUserQuery() error = %v", err)
	}

	if req.RoleType != "TEMPORARY" {
		t.Fatalf("RoleType = %q, want TEMPORARY", req.RoleType)
	}
}

func TestParseRoleQueryDefaults(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	req, err := parseRoleQuery(r)
	if err != nil {
		t.Fatalf("parseRoleQuery() error = %v", err)
	}

	if !req.AddRoles || !req.AddPermissions || !req.AddTotalUsers {
		t.Fatalf("role expansion defaults = roles:%v permissions:%v total_users:%v", req.AddRoles, req.AddPermissions, req.AddTotalUsers)
	}
	if req.Limit != defaultListLimit {
		t.Fatalf("Limit = %d, want %d", req.Limit, defaultListLimit)
	}
}

func TestParsePermissionQueryDataFilters(t *testing.T) {
	r := httptest.NewRequest("GET", "/?data.tenant=acme&data.region=eu&data=ignored", nil)
	req, err := parsePermissionQuery(r)
	if err != nil {
		t.Fatalf("parsePermissionQuery() error = %v", err)
	}

	if !req.AddRoles {
		t.Fatal("AddRoles = false, want true")
	}
	if len(req.Data) != 2 || req.Data["tenant"] != "acme" || req.Data["region"] != "eu" {
		t.Fatalf("Data = %#v, want tenant and region filters", req.Data)
	}
}

func TestGetUsersAPIRejectsInvalidQuery(t *testing.T) {
	r := httptest.NewRequest("GET", "/?_limit=invalid", nil)
	w := httptest.NewRecorder()

	(&Auth{}).GetUsersAPI(w, r)

	if w.Code != 400 {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}
