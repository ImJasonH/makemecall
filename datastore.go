package app

import (
	"errors"
	"math/rand"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"
)

///////////
// USERS //
///////////

type User struct {
	PhoneNumber string `datastore:",noindex"` // Also the key.
	ZipCode     string `datastore:",noindex"`
	NextCall    time.Time
}

func (u User) NextCallFormatted() string {
	return u.NextCall.In(nytz).Format(timeFmt)
}

func isNotUser(err error) bool {
	return err == datastore.ErrNoSuchEntity
}

func GetUser(ctx context.Context, n string) (*User, error) {
	k := datastore.NewKey(ctx, "User", n, 0, nil)
	var u User
	if err := datastore.Get(ctx, k, &u); err == datastore.ErrNoSuchEntity {
		return nil, err
	} else if err != nil {
		log.Errorf(ctx, "GetUser: Get(%q): %v", n, err)
		return nil, err
	}
	return &u, nil
}

func InsertUser(ctx context.Context, n, zip string) (*User, error) {
	k := datastore.NewKey(ctx, "User", n, 0, nil)
	u := User{
		PhoneNumber: n,
		ZipCode:     zip,
		NextCall:    someTimeTomorrow(),
	}
	if _, err := datastore.Put(ctx, k, &u); err != nil {
		log.Errorf(ctx, "InsertUser: Put(%q): %v", n, err)
		return nil, err
	}
	log.Infof(ctx, "Stored user: %s", n)
	return &u, nil
}

func CallableUsers(ctx context.Context) ([]User, error) {
	var us []User
	now := time.Now().In(nytz)
	q := datastore.NewQuery("User").
		Filter("NextCall <", now).
		Order("-NextCall").
		Limit(100)

	n, err := q.Count(ctx)
	if err != nil {
		log.Errorf(ctx, "Count: %v", err)
		return nil, err
	}
	log.Debugf(ctx, "Callable users before now (%s): %d", now, n)

	for t := q.Run(ctx); ; {
		var u User
		if _, err := t.Next(&u); err == datastore.Done {
			break
		} else if err != nil {
			log.Errorf(ctx, "Query: %v", err)
			return nil, err
		}
		us = append(us, u)
	}
	return us, nil
}

func SetNextCall(ctx context.Context, n string, next time.Time) (*User, error) {
	var u User
	if err := datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		k := datastore.NewKey(ctx, "User", n, 0, nil)
		if err := datastore.Get(ctx, k, &u); err != nil {
			log.Errorf(ctx, "SetNextCall(%s): Get: %v", n, err)
			return err
		}
		u.NextCall = next
		log.Infof(ctx, "User %s will call tomorrow at %s", n, next)
		if _, err := datastore.Put(ctx, k, &u); err != nil {
			log.Errorf(ctx, "SetNextCall(%s): Put: %v", n, err)
			return err
		}
		return nil
	}, nil); err != nil {
		return nil, err
	}
	return &u, nil
}

func DeleteUser(ctx context.Context, n string) {
	k := datastore.NewKey(ctx, "User", n, 0, nil)
	if err := datastore.Delete(ctx, k); err != nil {
		log.Errorf(ctx, "Error deleting %s: %v", n, err)
	}
	log.Infof(ctx, "Deleted %s", n)
}

///////////
// CALLS //
///////////

type Call struct {
	Key      string `datastore:",noindex"`
	To       string `datastore:",noindex"`
	From     string `datastore:",noindex"`
	Sid      string // Twilio SID
	Created  time.Time
	Duration time.Duration `datastore:",noindex"`
	// "new" means not called yet, "skipped" means user SKIP'd, rest are Twilio statuses.
	Status string
}

const (
	callKeyLength = 10
	alphabet      = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890"
)

func randomString() string {
	s := make([]byte, callKeyLength)
	for i := 0; i < callKeyLength; i++ {
		s[i] = alphabet[rand.Intn(len(alphabet))]
	}
	return string(s)
}

func InsertCall(ctx context.Context, from, to string) (*Call, error) {
	key := randomString()
	pk := datastore.NewKey(ctx, "User", from, 0, nil)
	k := datastore.NewKey(ctx, "Call", key, 0, pk)
	now := time.Now()
	c := Call{
		Key:     key,
		To:      to,
		From:    from,
		Created: now,
		Status:  "new",
	}
	if _, err := datastore.Put(ctx, k, &c); err != nil {
		log.Errorf(ctx, "InsertCall: Put(%q): %v", key, err)
		return nil, err
	}
	log.Infof(ctx, "Inserted Call %s", key)
	return &c, nil
}

func GetCall(ctx context.Context, u User, key string) (*Call, error) {
	uk := datastore.NewKey(ctx, "User", u.PhoneNumber, 0, nil)
	k := datastore.NewKey(ctx, "Call", key, 0, uk)
	var c Call
	if err := datastore.Get(ctx, k, &c); err != nil {
		log.Errorf(ctx, "GetCall: Get(%q): %v", key, err)
		return nil, err
	}
	return &c, nil
}

func UpdateCallBySID(ctx context.Context, sid, status string, dur time.Duration) error {
	// Lookup Call key for SID.
	ck := lookupBySID(ctx, sid)
	if ck == nil {
		return errors.New("sid lookup failed")
	}

	return datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		var c Call
		if err := datastore.Get(ctx, ck, &c); err != nil {
			log.Errorf(ctx, "UpdateCallBySID(%q): Get: %v", sid, err)
			return err
		}

		c.Status = status
		log.Infof(ctx, "Updating status to %q", status)
		if dur != time.Duration(0) {
			c.Duration = dur
		}
		if _, err := datastore.Put(ctx, ck, &c); err != nil {
			log.Errorf(ctx, "UpdateCallBySID: Put(%q): %v", sid, err)
			return err
		}
		log.Infof(ctx, "Successful update")
		return nil
	}, nil)
}

// Sets SID on a call by key.
func SetSID(ctx context.Context, u User, callID, sid string) error {
	// Store the SIDLookup.
	storeSIDLookup(ctx, sid, u.PhoneNumber, callID)

	return datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		uk := datastore.NewKey(ctx, "User", u.PhoneNumber, 0, nil)
		k := datastore.NewKey(ctx, "Call", callID, 0, uk)
		var c Call
		if err := datastore.Get(ctx, k, &c); err != nil {
			log.Errorf(ctx, "SetSID(%s): Get: %v", callID, err)
			return err
		}
		c.Sid = sid
		if _, err := datastore.Put(ctx, k, &c); err != nil {
			log.Errorf(ctx, "SetSID(%s): Put: %v", callID, err)
			return err
		}
		return nil
	}, nil)
}

var ErrNoSkippableCalls = errors.New("no skippable calls")

// Skips the latest "new" call for the user, sets the user's NextCall time.
func SkipNextCall(ctx context.Context, n string) error {
	return datastore.RunInTransaction(ctx, func(ctx context.Context) error {
		uk := datastore.NewKey(ctx, "User", n, 0, nil)
		q := datastore.NewQuery("Call").
			Ancestor(uk).
			Order("-Created"). // Key is a timestamp.
			Limit(1)
		var c Call
		ck, err := q.Run(ctx).Next(&c)
		if err != nil {
			log.Errorf(ctx, "SkipNextCall(%s): Next: %v", n, err)
			return err
		}

		if c.Status != "new" {
			log.Infof(ctx, `Next call (%s) is not "new": %s`, c.Key, c.Status)
			return ErrNoSkippableCalls
		}

		log.Infof(ctx, "User's next call is %s", c.Key)
		c.Status = "skipped"
		if _, err := datastore.Put(ctx, ck, &c); err != nil {
			log.Errorf(ctx, "SkipNextCall(%s): Put: %v", n, err)
			return err
		}
		return nil
	}, nil)
}

////////////////
// SID LOOKUP //
////////////////

// SIDLookup is a simple mapping between Twilio SID and the corresponding Call key.
//
// It's necessary because when we get call statuses, the request doesn't
// include the originating user's phone number, which is necessary to construct
// a User key, which is the ancestor of a Call key. :(
//
// So, instead, this small Entity maps Twilio SID -> Call key.
type SIDLookup struct {
	// Key is SID
	CallKey *datastore.Key
}

func storeSIDLookup(ctx context.Context, sid, user, call string) {
	uk := datastore.NewKey(ctx, "User", user, 0, nil)
	ck := datastore.NewKey(ctx, "Call", call, 0, uk)
	luk := datastore.NewKey(ctx, "SIDLookup", sid, 0, nil)
	if _, err := datastore.Put(ctx, luk, &SIDLookup{ck}); err != nil {
		log.Errorf(ctx, "storeSIDLookup(%q): %v", sid, err)
	}
}

func lookupBySID(ctx context.Context, sid string) *datastore.Key {
	luk := datastore.NewKey(ctx, "SIDLookup", sid, 0, nil)
	var lu SIDLookup
	if err := datastore.Get(ctx, luk, &lu); err != nil {
		log.Errorf(ctx, "lookupBySID(%q): %v", sid, err)
	}
	return lu.CallKey
}
