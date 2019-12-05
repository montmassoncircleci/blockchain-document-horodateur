package main

import (
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"github.com/crewjam/saml/samlsp"
	"github.com/gorilla/csrf"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type RouteHandler struct {

}

/*
	Reverse Proxy Logic
*/

// Serve a reverse proxy for a given url
func serveReverseProxy(target string, res http.ResponseWriter, req *http.Request) {
	// parse the url
	url, _ := url.Parse(target)

	// create the reverse proxy
	proxy := httputil.NewSingleHostReverseProxy(url)

	// Update the headers to allow for SSL redirection
	req.URL.Host = url.Host
	req.URL.Scheme = url.Scheme
	req.Header.Set("X-Forwarded-Host", req.Header.Get("Host"))
	req.Host = url.Host

	// Note that ServeHttp is non blocking and uses a go routine under the hood
	proxy.ServeHTTP(res, req)
}

func (this *RouteHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if(r.Method != http.MethodGet && r.Method != http.MethodPost) {
		w.WriteHeader(444)
		return
	}

	mainURI := os.Getenv("MAIN_URI")

	path := r.URL.Path[1:]

	if strings.Split(path, "/")[0] != mainURI {
		http.Redirect(w, r, "https://www.ge.ch/dossier/geneve-numerique/blockchain", 308)
		return
	}

	path = strings.TrimPrefix(path, mainURI+"/")

	indexToServe := path

	if path == "" {
		indexToServe = "index.html"
	}

	_, err := ioutil.ReadFile("mockup/" + string(indexToServe))


	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline' 'self'; connect-src 'self'; img-src data: *; style-src 'unsafe-inline' *; font-src *;")
	w.Header().Set("X-XSS-Protection", "1; mode=block")
	// If indexToServe is a valid file then return the file
	// Otherwise serve API if uri == /api/*
	// Finally redirect if incorrect request
	if err == nil {
		w.Header().Set("X-CSRF-Token", csrf.Token(r))

		http.ServeFile(w, r, "mockup/"+string(indexToServe))
	} else if strings.Split(path, "/")[0] == "api" {
		if (strings.Split(path, "/")[1] == "swagger.json") {
			w.WriteHeader(404)
		} else {
			w.Header().Set("X-CSRF-Token", csrf.Token(r))

			r.URL.Path = "/" + strings.TrimPrefix(r.URL.Path, "/"+mainURI+"/api/") // Remove api from uri

			apiHost := os.Getenv("API_HOST")

			serveReverseProxy("http://"+apiHost, w, r)
		}
	} else {
		http.Redirect(w, r, "https://www.ge.ch/dossier/geneve-numerique/blockchain", 308)
	}
}

func main() {
	keyName := os.Getenv("KEY_NAME")

	csrfLimitString := os.Getenv("CSRF_TIME_LIMIT")
	if csrfLimitString == "" {
		log.Fatalf("CSRF limit is not specified")
	}
	csrfLimit, err := strconv.Atoi(csrfLimitString)
	if err != nil {
		log.Fatalf("could not convert CSRF limit to int : %v", err.Error())
	}
	if csrfLimit < 5 * 60 {
		log.Fatalf("CSRF limit should be at least 300 seconds")
	}

	keyPair, err := tls.LoadX509KeyPair(keyName+".cert", keyName+".key")
	if err != nil {
		log.Fatal(err)
	}
	keyPair.Leaf, err = x509.ParseCertificate(keyPair.Certificate[0])
	if err != nil {
		log.Fatal(err)
	}

	idpEnv := os.Getenv("IDP_METADATA")

	idpMetadataURL, err := url.Parse(idpEnv)
	if err != nil {
		log.Fatal(err)
	}

	spEnv := os.Getenv("SP_URL")

	rootURL, err := url.Parse(spEnv)
	if err != nil {
		log.Fatal(err)
	}

	samlSP, _ := samlsp.New(samlsp.Options{
		URL:            *rootURL,
		Key:            keyPair.PrivateKey.(*rsa.PrivateKey),
		Certificate:    keyPair.Leaf,
		IDPMetadataURL: idpMetadataURL,
		CookieSecure:	true,
	})


	mainURI := os.Getenv("MAIN_URI")

	// This is where the SAML package will open information about SP to the world
	http.Handle("/"+mainURI+"/saml/", samlSP)

	CSRF := csrf.Protect([]byte("32-byte-long-auth-key"), csrf.MaxAge(csrfLimit))

	// Main Gateway to Webapp & API, it needs SAML login
	http.Handle("/", samlSP.RequireAccount(http.HandlerFunc(CSRF(new(RouteHandler)).ServeHTTP)))

	if err := http.ListenAndServe(":8080", nil); err != nil {
		panic(err)
	}

	log.Println("HTTP running on 8080")
}
