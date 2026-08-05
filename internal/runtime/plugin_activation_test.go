package runtime

import (
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/extensions"
	pluginpkg "github.com/blueberrycongee/wuu/internal/plugin"
)

func TestActivatedPluginsFailClosed(t *testing.T) {
	community := pluginpkg.Plugin{
		Manifest:             pluginpkg.Manifest{ID: "community"},
		SubjectID:            "project:community",
		Fingerprint:          "sha256:current",
		EffectivePermissions: []string{extensions.PermProcessSpawn},
	}
	official := community
	official.SubjectID = "bundled:official"
	official.ID = "official"
	official.Official = true

	if got := activatedPlugins(config.Config{}, []pluginpkg.Plugin{community}); len(got) != 0 {
		t.Fatalf("pending package activated: %+v", got)
	}
	if got := activatedPlugins(config.Config{}, []pluginpkg.Plugin{official}); len(got) != 1 {
		t.Fatalf("official package did not activate: %+v", got)
	}

	settings := &extensions.Settings{}
	if err := settings.RecordGrant(extensions.Grant{
		SubjectID: community.SubjectID, Fingerprint: community.Fingerprint,
		Scope: extensions.GrantScopeProject, Permissions: community.EffectivePermissions,
		ApprovedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if got := activatedPlugins(config.Config{Extensions: settings}, []pluginpkg.Plugin{community}); len(got) != 1 {
		t.Fatalf("exactly granted package did not activate: %+v", got)
	}

	changed := community
	changed.Fingerprint = "sha256:changed"
	if got := activatedPlugins(config.Config{Extensions: settings}, []pluginpkg.Plugin{changed}); len(got) != 0 {
		t.Fatalf("changed package activated using stale grant: %+v", got)
	}

	insufficient := &extensions.Settings{}
	if err := insufficient.RecordGrant(extensions.Grant{
		SubjectID: community.SubjectID, Fingerprint: community.Fingerprint,
		Scope: extensions.GrantScopeProject, ApprovedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if got := activatedPlugins(config.Config{Extensions: insufficient}, []pluginpkg.Plugin{community}); len(got) != 0 {
		t.Fatalf("package activated without all effective permissions: %+v", got)
	}

	if err := settings.RecordRejection(community.SubjectID, community.Fingerprint); err != nil {
		t.Fatal(err)
	}
	if got := activatedPlugins(config.Config{Extensions: settings}, []pluginpkg.Plugin{community}); len(got) != 0 {
		t.Fatalf("rejected package activated: %+v", got)
	}

	settings.Revoke(community.SubjectID)
	settings.SetDisabled(community.SubjectID, true)
	if got := activatedPlugins(config.Config{Extensions: settings}, []pluginpkg.Plugin{community}); len(got) != 0 {
		t.Fatalf("disabled package activated: %+v", got)
	}
	settings.SetDisabled(official.SubjectID, true)
	if got := activatedPlugins(config.Config{Extensions: settings}, []pluginpkg.Plugin{official}); len(got) != 0 {
		t.Fatalf("disabled official package activated: %+v", got)
	}
}
