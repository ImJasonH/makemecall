package app

import (
	"encoding/xml"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/context"
	"google.golang.org/appengine/log"
	"google.golang.org/appengine/urlfetch"
)

const twilioBaseURL = "https://api.twilio.com"

func respond(ctx context.Context, w http.ResponseWriter, r *Response) {
	w.Header().Set("Content-Type", "application/xml")
	b, err := xml.MarshalIndent(r, "", " ")
	if err != nil {
		log.Errorf(ctx, "xml.MarshalIndent: %v", err)
	}
	s := string(b)
	log.Infof(ctx, "Responding: %s", s)
	io.WriteString(w, s)
}

type Response struct {
	XMLName xml.Name `xml:"Response"`
	Verbs   []Verb
}

type Verb interface {
	isVerb()
}
type SMS struct {
	XMLName xml.Name `xml:"Message"`
	Text    string   `xml:",chardata"`
}

func (SMS) isVerb() {}

type Dial struct {
	XMLName xml.Name `xml:"Dial"`
	Number  Number
}

func NewDial(n string) Dial {
	return Dial{
		Number: Number{
			Number:              n,
			StatusCallback:      host + "/callstatus",
			StatusCallbackEvent: "initiated ringing answered completed",
		},
	}
}

func (Dial) isVerb() {}

type Number struct {
	XMLName             xml.Name `xml:"Number"`
	Number              string   `xml:",chardata"`
	StatusCallback      string   `xml:"statusCallback,attr,omitempty"`
	StatusCallbackEvent string   `xml:"statusCallbackEvent,attr,omitempty"`
}

type Say struct {
	XMLName  xml.Name `xml:"Say"`
	Text     string   `xml:",chardata"`
	Voice    string   `xml:"voice,attr"`
	Language string   `xml:"language,attr"`
}

func NewSay(s string) Say {
	return Say{
		Text:     s,
		Voice:    "female",
		Language: "en-gb",
	}
}

func (Say) isVerb() {}

// TODO: Use message feedback to ensure delivery: https://www.twilio.com/docs/api/rest/message/feedback
func SendSMS(ctx context.Context, to, text string) {
	v := &url.Values{}
	v.Set("To", to)
	v.Set("From", twilioNumber)
	v.Set("Body", text)
	req, err := http.NewRequest("POST", twilioBaseURL+"/2010-04-01/Accounts/"+sid+"/Messages", strings.NewReader(v.Encode()))
	if err != nil {
		log.Errorf(ctx, "NewRequest: %v", err)
		return
	}
	do(ctx, req)
}

func SendCall(ctx context.Context, to, dial string) string {
	v := &url.Values{}
	v.Set("To", to)
	v.Set("From", twilioNumber)
	v.Set("Url", host+"/connect?dial="+dial)
	req, err := http.NewRequest("POST", twilioBaseURL+"/2010-04-01/Accounts/"+sid+"/Calls", strings.NewReader(v.Encode()))
	if err != nil {
		log.Errorf(ctx, "NewRequest: %v", err)
		return ""
	}
	body := do(ctx, req)

	call := struct {
		Sid string `xml:"Call>Sid"`
	}{}
	if err := xml.Unmarshal(body, &call); err != nil {
		log.Errorf(ctx, "Unmarshal: %v", err)
		return ""
	}
	return call.Sid
}

// do adds auth, sends request, logs errors and responses.
func do(ctx context.Context, req *http.Request) []byte {
	req.SetBasicAuth(sid, tok)
	resp, err := urlfetch.Client(ctx).Do(req)
	if err != nil {
		log.Errorf(ctx, "urlfetch.Do: %v", err)
		return nil
	}
	defer resp.Body.Close()
	all, _ := ioutil.ReadAll(resp.Body)
	if resp.StatusCode >= http.StatusBadRequest {
		log.Errorf(ctx, "Twilio response (%d): %s", resp.StatusCode, string(all))
	} else {
		log.Infof(ctx, "Twilio response (200): %s", string(all))
	}
	return all
}
