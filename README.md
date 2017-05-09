# Make Me Call

Daily automated calls to your congresspeople.

**Make Me Call is no longer available**, but you can browse the source here, or
contact me to start running your own instance.

## How to run your own.

Sign up for [Twilio](https://twilio.com), buy a phone number.

Add `config.go` in `package app` which defines:

*   `sid`: Your Twilio account SID
*   `tok`: Your Twilio account token
*   `twilioNumber`: Your Twilio phone number

Deploy to App Engine:

```
gcloud app deploy *.yaml --version=1
```

**This project is not owned by or affiliated with Google, Inc., in any way. It
is wholly owned and operated by me.**
