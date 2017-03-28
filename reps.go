package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/net/context"
	"google.golang.org/appengine/log"
	"google.golang.org/appengine/urlfetch"
)

type LookupResponse struct {
	Results []Rep `json:"results"`
}

type Rep struct {
	Name        string `json:"name"`
	PhoneNumber string `json:"phone"`
	Party       string `json:"party"`
	State       string `json:"state"`
	District    string `json:"district"`
	Link        string `json:"link"`
}

func (r Rep) Title() string {
	if strings.Contains(r.Link, "senate.gov") {
		return "Sen. "
	}
	if strings.Contains(r.Link, "house.gov") {
		return "Rep. "
	}
	return ""
}

func (r Rep) String() string {
	return fmt.Sprintf("%s%s (%s)", r.Title(), r.Name, r.Party)
}

func LookupReps(ctx context.Context, zip string) []Rep {
	client := urlfetch.Client(ctx)
	resp, err := client.Get("http://whoismyrepresentative.com/getall_mems.php?output=json&zip=" + zip)
	if err != nil {
		log.Errorf(ctx, "LookupReps(%s): %v", zip, err)
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		log.Errorf(ctx, "LookupReps(%s): returned %d", zip, resp.StatusCode)
		return nil
	}
	defer resp.Body.Close()
	var r LookupResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		log.Errorf(ctx, "json.Decode: %v", err)
		return nil
	}
	return r.Results
}
