package yadisk

import (
	"crypto/sha256"
	"encoding/base64"
	"html"
	"net/http"
	"strings"
)

// The pages of the loopback receiver. Each response is one card of the
// approved look, in the dark theme of the app: no script, no link, no
// external resource and nothing taken from the request. The CSS is inline,
// allowed by the hash of its text in the Content-Security-Policy.

// pageCSS is the text inside <style>; pageCSP is computed from it.
const pageCSS = `
:root { color-scheme: dark; }
html, body { margin: 0; min-height: 100%; background: #4e5161; color: #e6e8f2; font: 17px/1.45 "Liberation Sans", Arial, Helvetica, sans-serif; }
body { display: flex; align-items: center; justify-content: center; padding: 3rem 1rem; box-sizing: border-box; min-height: 100vh; }
.card {
  width: 22rem; max-width: 100%; box-sizing: border-box; padding: 2rem 2rem 1.75rem; border-radius: 10px;
  background: #464958; border: 1px solid #393c49; box-shadow: 0 12px 32px rgb(0 0 0 / .28); text-align: center;
}
.logo { width: 72px; height: 72px; display: block; margin: 0 auto .75rem; }
.brand { margin: 0 0 1.25rem; font-size: .95rem; letter-spacing: .06em; color: #8e92a3; text-transform: uppercase; }
.mark { width: 44px; height: 44px; display: block; margin: 0 auto .75rem; }
h1 { margin: 0 0 .5rem; font-size: 1.35rem; font-weight: 700; }
p { margin: 0; color: #8e92a3; }
.hint { margin-top: 1.25rem; padding-top: 1rem; border-top: 1px solid #393c49; font-size: .9rem; }
`

// pageCSP allows exactly pageCSS and nothing else.
var pageCSP = func() string {
	sum := sha256.Sum256([]byte(pageCSS))

	return "default-src 'none'; style-src 'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) +
		"'; img-src 'none'; frame-ancestors 'none'"
}()

// logoSVG is build/appicon.svg without its size, title and namespace; the
// colors are literals.
const logoSVG = `<svg class="logo" viewBox="0 0 256 256" aria-hidden="true">` +
	`<defs>` +
	`<linearGradient id="c1" x1="0" y1="14" x2="0" y2="242" gradientUnits="userSpaceOnUse"><stop stop-color="#a06446"/><stop offset="1" stop-color="#84472a"/></linearGradient>` +
	`<linearGradient id="s1" x1="0" y1="14" x2="0" y2="242" gradientUnits="userSpaceOnUse"><stop stop-color="#343434"/><stop offset="1" stop-color="#1e1e1e"/></linearGradient>` +
	`<clipPath id="b1"><path d="M 51 14 H 203 C 214 14 223 23 223 34 V 222 C 223 233 214 242 203 242 H 51 C 40 242 31 233 31 222 V 34 C 31 23 40 14 51 14 Z"/></clipPath>` +
	`<mask id="k1" maskUnits="userSpaceOnUse" x="0" y="0" width="256" height="256"><rect width="256" height="256" fill="white"/>` +
	`<path d="M 150 156.5 C 146.4 156.5 143.5 159.4 143.5 163 C 143.5 165.4 144.8 167.5 146.8 168.6 V 182 H 153.2 ` +
	`V 168.6 C 155.2 167.5 156.5 165.4 156.5 163 C 156.5 159.4 153.6 156.5 150 156.5 Z" fill="black"/></mask>` +
	`</defs>` +
	`<g clip-path="url(#b1)">` +
	`<path d="M 51 14 H 203 C 214 14 223 23 223 34 V 222 C 223 233 214 242 203 242 H 51 C 40 242 31 233 31 222 V 34 C 31 23 40 14 51 14 Z" fill="url(#c1)"/>` +
	`<rect x="31" y="14" width="46" height="228" fill="url(#s1)"/>` +
	`<path d="M 127 14 H 172 V 92 L 149.5 78 L 127 92 Z" fill="#df9e25"/></g>` +
	`<g fill="#2c2c2b" mask="url(#k1)">` +
	`<path d="M 126 141 V 131 C 126 118 136.5 107.5 149.5 107.5 C 162.5 107.5 173 118 173 131 V 148 H 163 V 131 C 163 123.5 157 117.5 149.5 117.5 C 142 117.5 136 123.5 136 131 V 141 Z"/>` +
	`<path d="M 118 148 H 182 V 188 H 118 Z"/></g></svg>`

// The result marks, 48x48, with literal colors.
const (
	markOK = `<svg class="mark" viewBox="0 0 48 48" aria-hidden="true"><circle cx="24" cy="24" r="22" fill="none" stroke="#7fb37a" stroke-width="3"/>` +
		`<path d="M14 25 l7 7 l13 -14" fill="none" stroke="#7fb37a" stroke-width="3.5" stroke-linecap="round" stroke-linejoin="round"/></svg>`
	markMuted = `<svg class="mark" viewBox="0 0 48 48" aria-hidden="true"><circle cx="24" cy="24" r="22" fill="none" stroke="#8e92a3" stroke-width="3"/>` +
		`<path d="M16 24 h16" fill="none" stroke="#8e92a3" stroke-width="3.5" stroke-linecap="round"/></svg>`
	markError = `<svg class="mark" viewBox="0 0 48 48" aria-hidden="true"><circle cx="24" cy="24" r="22" fill="none" stroke="#f87171" stroke-width="3"/>` +
		`<path d="M24 13 v14" stroke="#f87171" stroke-width="3.5" stroke-linecap="round"/><circle cx="24" cy="34" r="2.3" fill="#f87171"/></svg>`
)

// page is one card: its status, mark, title and text.
type page struct {
	status int
	mark   string
	title  string
	text   string
}

var (
	pageSignedIn = page{
		status: http.StatusOK, mark: markOK,
		title: "Signed in to Yandex Disk",
		text:  "Return to Smartnote and press OK in Settings to save the sign-in.",
	}
	pageCancelled = page{
		status: http.StatusOK, mark: markMuted,
		title: "Sign-in cancelled",
		text:  "Smartnote did not get access to Yandex Disk. You can sign in again from Settings.",
	}
	pageFailed = page{
		status: http.StatusOK, mark: markError,
		title: "Sign-in failed",
		text:  "Yandex did not complete the sign-in. Return to Smartnote and try again.",
	}
	pageExpired = page{
		status: http.StatusBadRequest, mark: markMuted,
		title: "Sign-in link expired",
		text:  "This sign-in link has expired or was already used. Start the sign-in again from Smartnote Settings.",
	}
	pageNotFound = page{
		status: http.StatusNotFound, mark: markMuted,
		title: "Page not found",
		text:  "This address is not part of Smartnote.",
	}
)

// body is the document of p; every part of it is a constant of this file.
func (p page) body() string {
	var b strings.Builder

	b.WriteString("<!doctype html>\n<html lang=\"en\"><head><meta charset=\"utf-8\">" +
		"<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\"><title>Smartnote</title>" +
		"<style>" + pageCSS + "</style></head><body><main class=\"card\">")
	b.WriteString(logoSVG)
	b.WriteString("<p class=\"brand\">Smartnote</p>")
	b.WriteString(p.mark)
	b.WriteString("<h1>" + html.EscapeString(p.title) + "</h1><p>" + html.EscapeString(p.text) + "</p>")
	b.WriteString("<p class=\"hint\">You can close this tab.</p></main></body></html>\n")

	return b.String()
}

// write answers with p and the headers every response of the receiver has.
// close ends the connection after the answer.
func (p page) write(w http.ResponseWriter, closeAfter bool) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", pageCSP)

	if closeAfter {
		h.Set("Connection", "close")
	}

	w.WriteHeader(p.status)
	_, _ = w.Write([]byte(p.body())) // the browser may be gone
}
