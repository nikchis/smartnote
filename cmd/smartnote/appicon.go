package main

import _ "embed"

// appIcon is build/appicon.png, the logo of the application and of the title
// bar; TestAppIconMatchesBuild keeps the copy equal to the source.
//
//go:embed appicon.png
var appIcon []byte
