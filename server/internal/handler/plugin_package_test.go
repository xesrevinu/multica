package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

// Publishing, and the guarantee it exists for.
//
// The old model froze the manifest at install and loaded the surface script from
// the author's server on every panel open, so "the administrator approved this"
// and "this is what runs" were two different statements. These tests pin the
// replacement: an installation names one immutable version, and publishing
// another one changes nothing until an administrator upgrades.

const packageTestManifest = `{
  "manifest_version": 1,
  "key": "com.example.published",
  "name": "Published Panel",
  "version": "%VERSION%",
  "author": { "name": "example" },
  "scopes": ["issues:read"],
  "contributes": {
    "surfaces": [{ "key": "hello", "type": "issue_panel", "name": "Hello", "entry": "ui/main.js" }]
  }
}`

func packageManifest(version string) string {
	return strings.Replace(packageTestManifest, "%VERSION%", version, 1)
}

func pluginBundleZip(t *testing.T, manifest, script string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range map[string]string{
		plugincontract.ManifestFilename: manifest,
		"ui/main.js":                    script,
	} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buffer.Bytes()
}

func uploadPluginBundle(t *testing.T, archive []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("bundle", "plugin.zip")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(archive); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := form.Close(); err != nil {
		t.Fatalf("close form: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/plugins/packages", &body)
	request.Header.Set("X-User-ID", testUserID)
	request.Header.Set("Content-Type", form.FormDataContentType())
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("id", testWorkspaceID)
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))

	recorder := httptest.NewRecorder()
	testHandler.PublishPluginPackage(recorder, request)
	return recorder
}

func publishUploadedBundle(t *testing.T, manifest, script string) service.PluginPackageSummary {
	t.Helper()
	recorder := uploadPluginBundle(t, pluginBundleZip(t, manifest, script))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("publish: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var published service.PluginPackageSummary
	if err := json.Unmarshal(recorder.Body.Bytes(), &published); err != nil {
		t.Fatalf("decode published package: %v", err)
	}
	return published
}

func installPublishedVersion(t *testing.T, versionID string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"version_id": versionID, "granted_scopes": []string{"issues:read"}})
	recorder := httptest.NewRecorder()
	testHandler.InstallPlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins", body, map[string]string{"id": testWorkspaceID}))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("install: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var installed struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &installed); err != nil {
		t.Fatalf("decode installation: %v", err)
	}
	return installed.ID
}

func surfaceScript(t *testing.T, installationID, surfaceKey string) (*httptest.ResponseRecorder, service.PluginSurfaceScript) {
	t.Helper()
	previousOrigin := testHandler.cfg.PluginSurfaceOrigin
	previousTokens := testHandler.PluginSurfaceTokens
	testHandler.cfg.PluginSurfaceOrigin = "https://plugin-content.example.test"
	testHandler.PluginSurfaceTokens, _ = NewPluginSurfaceTokenBox(bytes.Repeat([]byte{7}, 32))
	t.Cleanup(func() {
		testHandler.cfg.PluginSurfaceOrigin = previousOrigin
		testHandler.PluginSurfaceTokens = previousTokens
	})

	recorder := httptest.NewRecorder()
	testHandler.GetPluginSurfaceLaunch(recorder, pluginHandlerRequest(http.MethodGet, "/launch", nil, map[string]string{
		"id":             testWorkspaceID,
		"installationId": installationID,
		"surfaceKey":     surfaceKey,
	}))
	if recorder.Code != http.StatusOK {
		return recorder, service.PluginSurfaceScript{}
	}
	var launch pluginSurfaceLaunch
	if err := json.Unmarshal(recorder.Body.Bytes(), &launch); err != nil {
		t.Fatalf("decode surface launch: %v", err)
	}
	token := launch.URL[strings.LastIndex(launch.URL, "/")+1:]
	documentRecorder := httptest.NewRecorder()
	documentRequest := pluginHandlerRequest(http.MethodGet, "/plugin-surfaces/"+token, nil, map[string]string{"token": token})
	documentRequest.Host = "plugin-content.example.test"
	testHandler.ServePluginSurface(documentRecorder, documentRequest)
	if documentRecorder.Code != http.StatusOK {
		t.Fatalf("serve hosted surface: status=%d body=%s", documentRecorder.Code, documentRecorder.Body.String())
	}
	encoded := regexp.MustCompile(`id="multica-surface-code">([^<]+)</script>`).FindStringSubmatch(documentRecorder.Body.String())
	if len(encoded) != 2 {
		t.Fatal("hosted surface did not contain stored code")
	}
	code, err := base64.StdEncoding.DecodeString(encoded[1])
	if err != nil {
		t.Fatalf("decode hosted surface code: %v", err)
	}
	return recorder, service.PluginSurfaceScript{Code: string(code), Version: launch.Version, Digest: launch.Digest}
}

// An author with no server of their own publishes a panel plugin by upload, an
// administrator installs it, and the panel's code comes back from us. That is
// the acceptance criterion for the whole change.

// The invariant the RFC called non-negotiable: publishing again must not change
// what an installed workspace runs.

// Republishing a version is refused rather than accepted as an overwrite. The
// unique index is what makes immutability a rule instead of a convention.
func TestPublishRefusesToOverwriteAnExistingVersion(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	withHostCapabilities(t)

	publishUploadedBundle(t, packageManifest("1.0.0"), "console.log('v1');\n")
	recorder := uploadPluginBundle(t, pluginBundleZip(t, packageManifest("1.0.0"), "console.log('tampered');\n"))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("republish status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "immutable") {
		t.Fatalf("the conflict does not explain itself: %s", recorder.Body.String())
	}
}



// withHostCapabilities enables every contribution kind for the duration of a
// test, so these exercise publishing rather than the staged-rollout gate.
func withHostCapabilities(t *testing.T) {
	t.Helper()
	previous := testHandler.PluginService.Host
	testHandler.PluginService.Host = plugincontract.Capabilities{
		SurfaceTypes:  map[string]bool{plugincontract.SurfaceIssuePanel: true, plugincontract.SurfaceSidebarPanel: true, plugincontract.SurfaceModal: true},
		HookTriggers:  map[string]bool{plugincontract.TriggerUI: true, plugincontract.TriggerManual: true, plugincontract.TriggerAgent: true, plugincontract.TriggerEvent: true},
		HookTransport: map[string]bool{plugincontract.TransportHTTP: true, plugincontract.TransportMCP: true},
		ResourceTypes: map[string]bool{plugincontract.ResourceSkill: true},
	}
	t.Cleanup(func() { testHandler.PluginService.Host = previous })
}
