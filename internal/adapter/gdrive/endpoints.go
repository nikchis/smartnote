package gdrive

// Endpoints are the base URLs of the Drive API, of its uploads and of the
// Google token endpoint.
type Endpoints struct {
	API, Upload, OAuth string
}

// DefaultEndpoints are the production endpoints.
var DefaultEndpoints = Endpoints{
	API:    "https://www.googleapis.com/drive/v3",
	Upload: "https://www.googleapis.com/upload/drive/v3",
	OAuth:  "https://oauth2.googleapis.com",
}
