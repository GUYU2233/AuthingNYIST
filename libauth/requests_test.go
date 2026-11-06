package libauth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestExtractJSONFromJSONP(t *testing.T) {

	Convey("Extracting JSON from JSONP...", t, func() {
		json, err := extractJSONFromJSONP("cb()", "cb")
		So(err, ShouldBeNil)
		So(json, ShouldEqual, "")
		json, err = extractJSONFromJSONP("C({})", "C")
		So(err, ShouldBeNil)
		So(json, ShouldEqual, "{}")
		json, err = extractJSONFromJSONP(`jQuery({"key1": 1234})`, "jQuery")
		So(err, ShouldBeNil)
		So(json, ShouldEqual, `{"key1": 1234}`)
		json, err = extractJSONFromJSONP("C({})", "")
		So(err, ShouldNotBeNil)
		json, err = extractJSONFromJSONP("C({})", "Q")
		So(err, ShouldNotBeNil)
		json, err = extractJSONFromJSONP("C({}", "C")
		So(err, ShouldNotBeNil)
		json, err = extractJSONFromJSONP("", "C")
		So(err, ShouldNotBeNil)
	})
}

func TestAuthoritativeStatus(t *testing.T) {
	for _, tc := range []struct {
		body              string
		online, wantError bool
		username          string
	}{
		{`C_a_l_l_b_a_c_k({"error":"ok","user_name":"alice"});`, true, false, "alice"},
		{"not_online_error", false, false, ""},
		{`C_a_l_l_b_a_c_k({"error":"offline"})`, false, false, ""},
		{`C_a_l_l_b_a_c_k({"error":"ok"})`, false, true, ""},
		{`<html>proxy page</html>`, false, true, ""},
		{`C_a_l_l_b_a_c_k({"error":"unknown"})`, false, true, ""},
	} {
		t.Run(tc.body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/cgi-bin/rad_user_info" {
					t.Error("unexpected legacy fallback")
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			online, err, username := IsOnline(NewUrlProvider(strings.TrimPrefix(server.URL, "http://"), true), "1")
			if online != tc.online || (err != nil) != tc.wantError || username != tc.username {
				t.Fatalf("got %v %v %q", online, err, username)
			}
		})
	}
}

func TestCredentialsNotRedirectedOrReturnedInErrors(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("followed credential redirect") }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"?password=secret", http.StatusFound)
	}))
	defer server.Close()
	_, err := GetJSON(server.URL, url.Values{"password": {"secret"}, "info": {"sensitive"}})
	if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("unsafe redirect error: %v", err)
	}
	_, err = GetJSON("://invalid", url.Values{"password": {"secret"}})
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unsafe URL error: %v", err)
	}
}

// func TestBuildLoginParams(t *testing.T) {
// 	loggo.ConfigureLoggers("libauth=DEBUG")
// 	buildLoginParams("hello", "pass", "32f23b9c2229fd034f6d5160d8b4536496af550efc45113635689d2d8f12ffad")
// }
