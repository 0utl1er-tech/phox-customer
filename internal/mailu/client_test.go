package mailu

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestClient は httptest サーバに向けた Client を作る (token 固定)。
func newTestClient(h http.Handler) (*Client, *httptest.Server) {
	srv := httptest.NewServer(h)
	c := NewClient(srv.URL+"/api/v1", "test-token")
	return c, srv
}

func TestNewClient_Disabled(t *testing.T) {
	if NewClient("", "tok") != nil {
		t.Fatal("base 空なら nil を返すべき")
	}
	if NewClient("https://x", "") != nil {
		t.Fatal("token 空なら nil を返すべき")
	}
}

func TestCreateUser_SendsExpectedBody(t *testing.T) {
	var gotAuth, gotPath, gotMethod string
	var gotBody createUserBody
	c, srv := newTestClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotMethod = r.Method
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := c.CreateUser(context.Background(), "sales@0utl1er.tech", "pw123", "Sales")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/user" {
		t.Fatalf("unexpected %s %s", gotMethod, gotPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("auth header = %q", gotAuth)
	}
	if gotBody.Email != "sales@0utl1er.tech" || gotBody.RawPassword != "pw123" || gotBody.DisplayedName != "Sales" {
		t.Fatalf("body = %+v", gotBody)
	}
	if !gotBody.Enabled || !gotBody.EnableIMAP {
		t.Fatalf("enabled/enable_imap should be true: %+v", gotBody)
	}
	if gotBody.AllowSpoofing {
		t.Fatal("allow_spoofing should be false")
	}
}

func TestCreateUser_Conflict(t *testing.T) {
	c, srv := newTestClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer srv.Close()
	err := c.CreateUser(context.Background(), "dup@0utl1er.tech", "pw", "")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestSetPassword_EscapesEmailAndPatches(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody patchUserBody
	c, srv := newTestClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		gotMethod = r.Method
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	if err := c.SetPassword(context.Background(), "a+b@0utl1er.tech", "new-pw"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	if gotMethod != http.MethodPatch {
		t.Fatalf("method = %s", gotMethod)
	}
	if gotPath != "/api/v1/user/a+b@0utl1er.tech" {
		t.Fatalf("path = %q (email should be path-escaped)", gotPath)
	}
	if gotBody.RawPassword != "new-pw" {
		t.Fatalf("body = %+v", gotBody)
	}
}

func TestDeleteUser_NotFoundIgnorable(t *testing.T) {
	c, srv := newTestClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s", r.Method)
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	err := c.DeleteUser(context.Background(), "gone@0utl1er.tech")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDo_OtherErrorPropagates(t *testing.T) {
	c, srv := newTestClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()
	err := c.CreateUser(context.Background(), "x@0utl1er.tech", "pw", "")
	if err == nil || errors.Is(err, ErrConflict) || errors.Is(err, ErrNotFound) {
		t.Fatalf("expected generic error, got %v", err)
	}
}
