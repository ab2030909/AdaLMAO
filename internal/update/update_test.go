package update

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func server(t *testing.T, payload []byte, hash string, withSums bool) *httptest.Server {
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/repos/"+Repo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		assets := fmt.Sprintf(`{"name":%q,"browser_download_url":"%s/bin"}`, AssetName(), srv.URL)
		if withSums {
			assets += fmt.Sprintf(`,{"name":"checksums.txt","browser_download_url":"%s/sums"}`, srv.URL)
		}
		fmt.Fprintf(w, `{"tag_name":"v9.9.9","assets":[%s]}`, assets)
	})
	mux.HandleFunc("/bin", func(w http.ResponseWriter, r *http.Request) { w.Write(payload) })
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintf(w, "%s  %s\n", hash, AssetName()) })
	srv = httptest.NewServer(mux)
	t.Setenv("ADALMAO_UPDATE_API", srv.URL)
	return srv
}

func TestApplyVerifiedAndRefusals(t *testing.T) {
	payload := []byte("new-binary")
	sum := sha256.Sum256(payload)
	good := hex.EncodeToString(sum[:])

	dir := t.TempDir()
	exe := filepath.Join(dir, "adalmao.exe")
	os.WriteFile(exe, []byte("old-binary"), 0o755)

	// checksum mismatch -> refuse, binary untouched
	s := server(t, payload, "00"+good[2:], true)
	r, err := Latest()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(r, exe); err == nil {
		t.Fatal("mismatched checksum must be refused")
	}
	if b, _ := os.ReadFile(exe); string(b) != "old-binary" {
		t.Fatal("binary changed despite failed update")
	}
	s.Close()

	// no checksums.txt -> refuse
	s = server(t, payload, good, false)
	r, _ = Latest()
	if _, err := Apply(r, exe); err == nil {
		t.Fatal("missing checksums.txt must be refused")
	}
	s.Close()

	// good -> installs, keeps .old
	s = server(t, payload, good, true)
	defer s.Close()
	r, _ = Latest()
	old, err := Apply(r, exe)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new-binary" {
		t.Fatal("new binary not installed")
	}
	if b, _ := os.ReadFile(old); string(b) != "old-binary" {
		t.Fatal("old binary not preserved")
	}
}

func TestNewer(t *testing.T) {
	if !Newer("0.1.0", "v0.1.1") || Newer("0.1.1", "v0.1.1") || Newer("0.2.0", "0.1.9") {
		t.Fatal("version comparison wrong")
	}
}
