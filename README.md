# Make Me Call

Daily automated calls to your congresspeople.

## How to run your own.

Sign up for [Twilio](https://twilio.com), buy a phone number.

Add `config.go` in `package app` which defines:

* `sid`: Your Twilio account SID
* `tok`: Your Twilio account token
* `twilioNumber`: Your Twilio phone number

Deploy to App Engine:

```
gcloud app deploy *.yaml --version=1
```
