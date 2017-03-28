package app

import (
	"encoding/xml"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"google.golang.org/appengine/aetest"
)

const (
	userPhone = "8675309"
	zip       = "12345"
)

func isTomorrow(t time.Time) bool {
	// TODO: better
	return t.Day() > time.Now().Day()
}

func TestNewUser(t *testing.T) {
	ctx, done, err := aetest.NewContext()
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}
	defer done()

	// User doesn't exist yet.
	if u, err := GetUser(ctx, userPhone); err == nil {
		t.Errorf("GetUser returned %v, want err", u)
	}

	if u, err := InsertUser(ctx, userPhone, zip); err != nil {
		t.Errorf("InsertUser: %v", err)
	} else if !isTomorrow(u.NextCall) {
		t.Errorf("New user's NextCall isn't tomorrow: %s", u.NextCall)
	}

	// User exists now! \o/
	if u, err := GetUser(ctx, userPhone); err != nil {
		t.Errorf("GetUser returned %v, want err", u)
	}
}

func TestNow(t *testing.T) {
	t.Skip() // TODO: Fails because of delay package?
	ctx, done, err := aetest.NewContext()
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}
	defer done()
	inst, err := aetest.NewInstance(&aetest.Options{
		StronglyConsistentDatastore: true,
	})
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}
	defer inst.Close()

	// Insert user.
	if u, err := InsertUser(ctx, userPhone, zip); err != nil {
		t.Errorf("InsertUser: %v", err)
	} else if !isTomorrow(u.NextCall) {
		t.Errorf("New user's NextCall isn't tomorrow: %s", u.NextCall)
	}

	// Send a "NOW" text.
	v := &url.Values{}
	v.Set("From", userPhone)
	v.Set("Body", "NOW")
	req, err := inst.NewRequest("POST", "/incomingtext", strings.NewReader(v.Encode()))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	w := httptest.NewRecorder()
	incomingText(w, req)
	all, _ := ioutil.ReadAll(w.Body)
	if w.Code != http.StatusOK {
		t.Fatalf("Do (%d): %s", w.Code, string(all))
	}
	var r Response
	if err := xml.Unmarshal(all, &r); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got := r.Verbs[0].(*Say).Text; got != "" {
		t.Fatalf("NOW got response: %q", got)
	}
}
