package app

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"math/rand"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/appengine"
	"google.golang.org/appengine/delay"
	"google.golang.org/appengine/log"
	"google.golang.org/appengine/taskqueue"
)

const (
	host = "https://make-me-call.appspot.com"

	testNumber = "8887018992" // NOAA dial-a-buoy

	timeFmt = "Monday, January 02 at 3:04PM MST"

	tips = `Tips for calling:
- Give your name, city, and zip code.
- State an issue, state your opinion on it. That's it.
- Be nice. The person you're talking to has a hard job.
- Call every day so they remember you.
Text QUIT any time to stop.`
)

var nytz = mustLoadLocation()

func mustLoadLocation() *time.Location {
	l, err := time.LoadLocation("America/New_York")
	if err != nil {
		panic(err)
	}
	return l
}

func init() {
	http.HandleFunc("/incomingcall", incomingCall) // POSTed when someone calls.
	http.HandleFunc("/incomingtext", incomingText) // POSTed when someone texts.
	http.HandleFunc("/connect", connect)           // POSTed when user picks up call, Dials the other number in response.
	http.HandleFunc("/callstatus", callStatus)     // POSTed when call status changes.

	http.HandleFunc("/cron", cron)
}

func connect(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}
	ctx := appengine.NewContext(r)
	validateHMAC(ctx, r)

	r.ParseForm()
	log.Infof(ctx, "PostForm: %s", r.PostForm)

	dial := r.FormValue("dial")
	if dial == "" {
		log.Errorf(ctx, "Dial was not provided")
		dial = testNumber
	}

	w.Header().Set("Content-Type", "application/xml")
	respond(ctx, w, &Response{
		Verbs: []Verb{
			NewSay("Hello, you are now being connected."),
			NewDial(dial),
		},
	})
}

func incomingCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}
	ctx := appengine.NewContext(r)
	validateHMAC(ctx, r)

	r.ParseForm()
	log.Infof(ctx, "PostForm: %s", r.PostForm)

	respond(ctx, w, &Response{
		Verbs: []Verb{NewSay("Hello, thank you for calling. Text JOIN and your zip code to this number to get started.")},
	})
}

// validateHMAC checks incoming requests against X-Twilio-Signature header.
//
// https://www.twilio.com/docs/api/security
func validateHMAC(ctx context.Context, r *http.Request) {
	msg := r.URL.String()
	r.ParseForm()
	keys := []string{}
	for k, _ := range r.PostForm {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		msg += k + r.FormValue(k)
	}

	mac := hmac.New(sha1.New, []byte(tok))
	mac.Write([]byte(msg))
	got := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	want := r.Header.Get("X-Twilio-Signature")
	if got != want {
		// TODO: Make this a real error.
		log.Warningf(ctx, "Got %q, want %q", got, want)
	}
}

func incomingText(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(appengine.NewContext(r), 30*time.Second)
	defer cancel()
	validateHMAC(ctx, r)

	text := ""

	from := r.PostFormValue("From")
	body := strings.Trim(strings.ToUpper(r.PostFormValue("Body")), " ")
	log.Infof(ctx, "%s says: %s", from, body)

	if u, err := GetUser(ctx, from); isNotUser(err) {
		if isJoin(body) {
			zip := strings.Split(body, " ")[1]
			u, err = InsertUser(ctx, from, zip)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			text = `Thank you, you have joined!
Text QUIT any time to stop.
` + defaultText(ctx, u)
		} else {
			text = `Text "JOIN <ZIPCODE>" to get started.`
		}
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	} else {
		// User exists.
		switch body {
		case "TIPS":
			text = tips
		case "NOW":
			call.Call(ctx, *u, true)
		case "QUIT", "STOP":
			DeleteUser(ctx, from)
			text = `You quit. Text "JOIN <ZIPCODE>" at any time to get back in the fight.`
		case "SKIP":
			if err := SkipNextCall(ctx, from); err == ErrNoSkippableCalls {
				// TODO: Take this to mean "reschedule my as-yet-incoming call" ?
				// Until then, do nothing.
			} else if err != nil {
				// Do nothing.
			} else {
				next := someTimeTomorrow()
				if u, err := SetNextCall(ctx, from, next); err != nil {
					log.Errorf(ctx, "SetNextCall: %v", err)
				} else {
					log.Infof(ctx, "Successful SKIP")
					text = fmt.Sprintf("Your next call is %s", u.NextCallFormatted())
				}
			}
		default:
			text = defaultText(ctx, u)
		}
	}

	respond(ctx, w, &Response{
		Verbs: []Verb{&SMS{Text: text}},
	})
}

func defaultText(ctx context.Context, u *User) string {
	msg := fmt.Sprintf(`Your zip code is %s
Your next call is scheduled for %s
Your members of congress:
`, u.ZipCode, u.NextCallFormatted())
	// Look up reps by zip.
	rs := LookupReps(ctx, u.ZipCode)
	for _, r := range rs {
		msg += fmt.Sprintf("- %s\n", r.String())
	}
	return msg
}

var joinRE = regexp.MustCompile("JOIN [0-9]{5}(-[0-9]{4})?")

func isJoin(s string) bool {
	return joinRE.MatchString(s)
}

func cron(w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)
	if r.Method != "GET" {
		log.Errorf(ctx, "/cron got method %q", r.Method)
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}
	if hdr := r.Header.Get("X-Appengine-Cron"); hdr != "true" {
		log.Errorf(ctx, "/cron got header %q", hdr)
		http.Error(w, "", http.StatusForbidden)
		return
	}
	us, err := CallableUsers(ctx)
	if err != nil {
		// Don't return an error, that would cause us to be re-run.
		return
	}
	for _, u := range us {
		log.Infof(ctx, "User %q is callable", u.PhoneNumber)
		call.Call(ctx, u, false)
	}
}

var call = delay.Func("call", func(ctx context.Context, u User, force bool) {
	reps := LookupReps(ctx, u.ZipCode)
	if len(reps) == 0 {
		log.Errorf(ctx, "Zip %q had no reps", u.ZipCode)
		return
	}
	rand.Seed(time.Now().Unix())
	rep := reps[rand.Intn(len(reps))] // random rep

	// Insert a Call with status "new".
	c, err := InsertCall(ctx, u.PhoneNumber, rep.PhoneNumber)
	if err != nil {
		log.Errorf(ctx, "InsertCall: %v", err)
		return
	}

	SendSMS(ctx, u.PhoneNumber, fmt.Sprintf(`It's time for your call!
You will be calling %s.
Your call will come in five minutes. Get ready!
Text TIPS to get some tips.
Text SKIP to reschedule.`, rep.String()))

	task, err := doCall.Task(u, c.Key, rep)
	if err != nil {
		log.Errorf(ctx, "delay.Task: %v", err)
		return
	}
	task.Delay = 5 * time.Minute
	if _, err := taskqueue.Add(ctx, task, "default"); err != nil {
		log.Errorf(ctx, "taskqueue.Add: %v", err)
		return
	}
	log.Infof(ctx, "Enqueued actual-call task")
})

var doCall = delay.Func("actual-call", func(ctx context.Context, u User, callID string, rep Rep) {
	// Check whether the call has been skipped.
	c, err := GetCall(ctx, u, callID)
	if err != nil {
		log.Errorf(ctx, "GetCall: %v", err)
		return
	}
	if c.Status == "skipped" {
		log.Infof(ctx, "User %s skipped latest call %s, skipping", u.PhoneNumber, c.Key)
		return
	}
	if c.Status != "new" {
		log.Warningf(ctx, "Call %s was not new: %s", c.Key, c.Status)
	}

	log.Infof(ctx, "User %s will call %s", u.PhoneNumber, rep.PhoneNumber)

	// Send call and update associated SID.
	sid := SendCall(ctx, u.PhoneNumber, rep.PhoneNumber)
	SetSID(ctx, u, c.Key, sid)

	// Set next call for tomorrow.
	SetNextCall(ctx, u.PhoneNumber, someTimeTomorrow())
})

func callStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}
	ctx := appengine.NewContext(r)
	validateHMAC(ctx, r)

	// In call status, parentSID is the SID of the original call. Non-parent SID
	// is the child call to the rep.
	sid := r.FormValue("ParentCallSid")
	status := r.FormValue("CallStatus")

	log.Infof(ctx, "PostForm: %+v", r.PostForm)

	// Parse duration.
	var d time.Duration
	dur := r.FormValue("CallDuration")
	if dur != "" {
		i, _ := strconv.Atoi(dur)
		d = time.Duration(i) * time.Second
	}

	UpdateCallBySID(ctx, sid, status, d)
}

// someTimeTomorrow returns a time.Time tomorrow, between noon and 5pm EST.
//
// If today is Friday or Saturday, "tomorrow" actually means Monday.
func someTimeTomorrow() time.Time {
	now := time.Now().In(nytz)
	// If it's Friday, the next call should be Monday.
	addDays := 1
	switch now.Weekday() {
	case time.Friday:
		addDays = 3
	case time.Saturday:
		addDays = 2
	}
	noonTomorrow := time.Date(
		now.Year(),
		now.Month(),
		now.Day()+addDays,
		12, // noon
		0, 0, 0, nytz)

	// Add a random number of seconds between 0 and 5 hours.
	r := time.Duration(rand.Int63n(int64((5 * time.Hour).Seconds())))
	return noonTomorrow.Add(r * time.Second)
}
