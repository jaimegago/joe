package sessionauthz_test

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/sessionauthz"
)

// fakeResolver / fakeAdmin are minimal in-memory stand-ins so the decision
// model can be proven structurally without a database or the rbac package.
type fakeResolver struct {
	creator string
	found   bool
	err     error
}

func (f fakeResolver) SessionCreator(_ context.Context, _ string) (string, bool, error) {
	return f.creator, f.found, f.err
}

type fakeAdmin struct {
	admins map[string]bool
	err    error
}

func (f fakeAdmin) IsAdmin(_ context.Context, p string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.admins[p], nil
}

func newSeam(creator string, admins ...string) *sessionauthz.Seam {
	set := map[string]bool{}
	for _, a := range admins {
		set[a] = true
	}
	return sessionauthz.New(
		fakeResolver{creator: creator, found: true},
		fakeAdmin{admins: set},
	)
}

const sid = "sess-1"

// TestSeam_ReadIsTeamWide proves a read is granted to an authenticated
// principal who is neither the creator nor an admin (team-public read, §12.7).
func TestSeam_ReadIsTeamWide(t *testing.T) {
	s := newSeam("alice") // creator alice, no admins
	d, err := s.SessionAccess(context.Background(), "bob", sid, sessionauthz.ActionRead)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !d.Allowed {
		t.Fatalf("team-wide read: want allowed for non-owner non-admin, got deny")
	}
	if d.Relationship != sessionauthz.RelationshipTeamMember {
		t.Fatalf("relationship: want team_member, got %q", d.Relationship)
	}
}

// TestSeam_OwnerMutate proves owner-mutate is granted to the creator and denied
// to a non-creator non-admin principal (§12.7).
func TestSeam_OwnerMutate(t *testing.T) {
	s := newSeam("alice")
	for _, action := range []sessionauthz.Action{
		sessionauthz.ActionWrite, sessionauthz.ActionSoftDelete, sessionauthz.ActionRestore,
	} {
		owner, err := s.SessionAccess(context.Background(), "alice", sid, action)
		if err != nil || !owner.Allowed || owner.Relationship != sessionauthz.RelationshipOwner {
			t.Fatalf("%s: want allow as owner, got allowed=%v rel=%q err=%v",
				action, owner.Allowed, owner.Relationship, err)
		}
		other, err := s.SessionAccess(context.Background(), "bob", sid, action)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", action, err)
		}
		if other.Allowed {
			t.Fatalf("%s: want deny for non-creator non-admin, got allow", action)
		}
		if other.Relationship != sessionauthz.RelationshipTeamMember {
			t.Fatalf("%s: want team_member relationship, got %q", action, other.Relationship)
		}
	}
}

// TestSeam_AdminGovern proves admin-govern is granted to an admin and denied to
// a non-admin non-creator principal (§12.7). It also confirms a non-admin
// creator (owner) is denied governance — governance is admin-only.
func TestSeam_AdminGovern(t *testing.T) {
	s := newSeam("alice", "carol") // alice owner, carol admin
	for _, action := range []sessionauthz.Action{
		sessionauthz.ActionPurge, sessionauthz.ActionArchive,
		sessionauthz.ActionUnarchive, sessionauthz.ActionConfigureRetention,
	} {
		adminD, err := s.SessionAccess(context.Background(), "carol", sid, action)
		if err != nil || !adminD.Allowed || adminD.Relationship != sessionauthz.RelationshipAdmin {
			t.Fatalf("%s: want allow as admin, got allowed=%v rel=%q err=%v",
				action, adminD.Allowed, adminD.Relationship, err)
		}
		teamD, err := s.SessionAccess(context.Background(), "bob", sid, action)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", action, err)
		}
		if teamD.Allowed {
			t.Fatalf("%s: want deny for non-admin non-creator, got allow", action)
		}
		ownerD, err := s.SessionAccess(context.Background(), "alice", sid, action)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", action, err)
		}
		if ownerD.Allowed {
			t.Fatalf("%s: want deny for non-admin owner (governance is admin-only), got allow", action)
		}
	}
}

// TestSeam_AdminMayOwnerMutateCrossTenant proves admin is allowed owner-mutate
// actions on a session it does not own (§12.1 cross-tenant mutation, §12.5
// owner/admin restore). This is the nuance beyond the prompt's 3-bucket summary.
func TestSeam_AdminMayOwnerMutateCrossTenant(t *testing.T) {
	s := newSeam("alice", "carol")
	d, err := s.SessionAccess(context.Background(), "carol", sid, sessionauthz.ActionRestore)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !d.Allowed || d.Relationship != sessionauthz.RelationshipAdmin {
		t.Fatalf("admin cross-tenant restore: want allow as admin, got allowed=%v rel=%q",
			d.Allowed, d.Relationship)
	}
}

// TestSeam_UnknownActionDenies proves an unrecognized action denies.
func TestSeam_UnknownActionDenies(t *testing.T) {
	s := newSeam("alice", "carol")
	// Even the creator and an admin are denied an action outside the vocabulary.
	for _, p := range []string{"alice", "carol", "bob"} {
		d, err := s.SessionAccess(context.Background(), p, sid, sessionauthz.Action("frobnicate"))
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", p, err)
		}
		if d.Allowed {
			t.Fatalf("%s: unrecognized action must deny, got allow", p)
		}
	}
}

// TestSeam_NilPrincipalDenies proves an empty/unauthenticated principal denies
// every action, including read.
func TestSeam_NilPrincipalDenies(t *testing.T) {
	s := newSeam("alice", "carol")
	for _, action := range []sessionauthz.Action{
		sessionauthz.ActionRead, sessionauthz.ActionWrite, sessionauthz.ActionPurge,
	} {
		d, err := s.SessionAccess(context.Background(), "", sid, action)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", action, err)
		}
		if d.Allowed || d.Relationship != sessionauthz.RelationshipNone {
			t.Fatalf("%s: empty principal must deny with relationship none, got allowed=%v rel=%q",
				action, d.Allowed, d.Relationship)
		}
	}
}

// TestSeam_DependencyErrorFailsClosed proves an admin-store error fails closed
// (denies) and surfaces the error so a caller can 500 distinctly.
func TestSeam_DependencyErrorFailsClosed(t *testing.T) {
	boom := errors.New("rbac store down")
	s := sessionauthz.New(fakeResolver{creator: "alice", found: true}, fakeAdmin{err: boom})
	d, err := s.SessionAccess(context.Background(), "alice", sid, sessionauthz.ActionRead)
	if err == nil {
		t.Fatalf("want non-nil error on dependency failure")
	}
	if d.Allowed {
		t.Fatalf("want deny on dependency failure (fail closed), got allow")
	}
}

// TestSeam_ReachesNoZoneOrPolicyMachinery is the structural proof that the seam
// stays separate from the component RBAC accessor and consults no zones or
// policies (§12.7). It parses every non-test source file in this package and
// asserts none of them import the component RBAC accessor, the rbac engine, or
// the graph/adapter stack. The seam's only authorization dependency is the
// AdminChecker interface, satisfied structurally by rbac.Repository without an
// import here.
func TestSeam_ReachesNoZoneOrPolicyMachinery(t *testing.T) {
	forbidden := []string{
		"github.com/jaimegago/joe/internal/access",
		"github.com/jaimegago/joe/internal/rbac",
		"github.com/jaimegago/joe/internal/graph",
		"github.com/jaimegago/joe/internal/adapters",
	}

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.ImportsOnly)
		if perr != nil {
			t.Fatalf("parse %s: %v", e.Name(), perr)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			for _, bad := range forbidden {
				if path == bad || strings.HasPrefix(path, bad+"/") {
					t.Errorf("%s imports %q — the session authz seam must reach no zone/policy "+
						"or component-RBAC machinery (§12.7). Its only authz dependency is the "+
						"AdminChecker interface (D-0011), injected without importing rbac.",
						e.Name(), path)
				}
			}
		}
	}
}
