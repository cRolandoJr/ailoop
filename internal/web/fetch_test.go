package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSinAllowlistVaACualquierLadoPublico(t *testing.T) {
	// Restringir destinos no defiende del canal real -lo que sale es la
	// pregunta- y vuelve inutil la investigacion: no se sabe de antemano
	// donde esta la respuesta.
	f := &Fetcher{}
	for _, u := range []string{
		"https://example.com/doc",
		"https://stackoverflow.com/questions/1",
		"https://un-blog-cualquiera.dev/post",
	} {
		if err := f.Allows(u); err != nil {
			t.Errorf("%s rechazado sin allowlist: %v", u, err)
		}
	}
}

func TestSinAllowlistIGUAL_SeBloqueaLoInterno(t *testing.T) {
	// El limite que queda no es sobre el proyecto sino sobre la RED: el
	// investigador corre dentro del perimetro aunque no tenga nada local.
	f := &Fetcher{}
	internos := []string{
		"http://192.168.1.1/",
		"http://localhost:9090/metrics",
		"http://169.254.169.254/latest/meta-data/",
		"http://nas.local/",
	}
	for _, u := range internos {
		if err := f.Allows(u); !errors.Is(err, ErrInternalTarget) {
			t.Errorf("%s: err = %v, quiero ErrInternalTarget", u, err)
		}
	}
}

func TestLaBlocklistSeRespetaAunSinAllowlist(t *testing.T) {
	f := &Fetcher{Blocked: []string{"sitio-prohibido.com"}}
	if err := f.Allows("https://sitio-prohibido.com/x"); err == nil {
		t.Error("no respeto la blocklist")
	}
	if err := f.Allows("https://sub.sitio-prohibido.com/x"); err == nil {
		t.Error("la blocklist no cubrio el subdominio")
	}
	if err := f.Allows("https://otro.com/x"); err != nil {
		t.Errorf("bloqueo algo que no estaba en la lista: %v", err)
	}
}

func TestElAllowlistOpcionalCubreSubdominios(t *testing.T) {
	// Sigue disponible para un proyecto que quiera restringir.
	f := &Fetcher{Allowed: []string{"go.dev"}}
	if err := f.Allows("https://pkg.go.dev/net/http"); err != nil {
		t.Errorf("rechazo un subdominio permitido: %v", err)
	}
	if err := f.Allows("https://go.dev/doc"); err != nil {
		t.Errorf("rechazo el dominio permitido: %v", err)
	}
	// Y no confunde un sufijo con un subdominio.
	if err := f.Allows("https://malicioso-go.dev/x"); err == nil {
		t.Error("acepto un dominio que solo TERMINA parecido")
	}
	if err := f.Allows("https://otro.com/x"); err == nil {
		t.Error("acepto un dominio fuera del allowlist")
	}
}

func TestNoSeAlcanzanDireccionesInternas(t *testing.T) {
	// Pedirle a localhost o al endpoint de metadata no es "traer
	// documentacion": es SSRF con pasos extra.
	f := &Fetcher{Allowed: []string{"localhost", "169.254.169.254", "10.0.0.1", "example.com"}}
	internos := []string{
		"http://localhost:8080/admin",
		"http://127.0.0.1/",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.1/",
		"http://192.168.1.1/",
		"http://algo.internal/",
	}
	for _, u := range internos {
		err := f.Allows(u)
		if !errors.Is(err, ErrInternalTarget) {
			t.Errorf("%s: err = %v, quiero ErrInternalTarget", u, err)
		}
	}
}

func TestSoloHttpYHttps(t *testing.T) {
	f := &Fetcher{Allowed: []string{"example.com"}}
	for _, u := range []string{"file:///etc/passwd", "ftp://example.com/x", "gopher://example.com"} {
		if err := f.Allows(u); err == nil {
			t.Errorf("acepto el esquema de %q", u)
		}
	}
}

func TestFetchTraeElTextoYAcota(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("metodo = %s, quiero GET", r.Method)
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><head><style>p{color:red}</style></head>" +
			"<body><script>alert(1)</script><p>Primer parrafo.</p><p>Segundo.</p></body></html>"))
	}))
	defer srv.Close()

	f := &Fetcher{Allowed: []string{"127.0.0.1"}, HTTP: srv.Client()}
	// El servidor de prueba es interno a proposito: se saltea el chequeo con
	// un fetcher que no lo aplica, para probar el parseo.
	page, err := (&Fetcher{HTTP: srv.Client(), Allowed: []string{"example.com"}}).fetchNoCheck(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(page.Text, "Primer parrafo") || !strings.Contains(page.Text, "Segundo") {
		t.Errorf("perdio el texto:\n%s", page.Text)
	}
	if strings.Contains(page.Text, "alert(1)") || strings.Contains(page.Text, "color:red") {
		t.Errorf("dejo pasar script o style:\n%s", page.Text)
	}
	_ = f
}

func TestStripHTMLConservaLaForma(t *testing.T) {
	html := "<h1>Titulo</h1><p>Uno</p><p>Dos</p><ul><li>a</li><li>b</li></ul>"
	out := StripHTML(html)
	for _, q := range []string{"Titulo", "Uno", "Dos", "a", "b"} {
		if !strings.Contains(out, q) {
			t.Errorf("perdio %q:\n%s", q, out)
		}
	}
	if strings.Contains(out, "<") {
		t.Errorf("quedaron tags:\n%s", out)
	}
}

func TestStripHTMLResuelveEntidades(t *testing.T) {
	if got := StripHTML("<p>a &amp; b &lt;c&gt;</p>"); !strings.Contains(got, "a & b <c>") {
		t.Errorf("no resolvio entidades: %q", got)
	}
}
