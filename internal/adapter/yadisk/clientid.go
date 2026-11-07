package yadisk

// BuiltinClientID is the client ID of the Smartnote app registered in
// Yandex ID (spec §6.1, §14). A client ID is public; the app has no client
// secret, PKCE protects the code exchange.
const BuiltinClientID = "543e36190e6549c8883212efb9582bc5"

// LoopbackAddr is the address of the sign-in receiver. Its redirect URI
// http://127.0.0.1:47123/callback is registered with the app.
const LoopbackAddr = "127.0.0.1:47123"
