package attach

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cRolandoJr/ailoop/internal/host"
)

func resolverEn(t *testing.T, archivos map[string]string) *Resolver {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range archivos {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return &Resolver{Workspace: dir, Host: host.Discover()}
}

func TestFindRefsNoConfundeUnMailConUnaReferencia(t *testing.T) {
	// "@" pegado a una palabra es parte de ella.
	casos := map[string][]string{
		"mira @main.go":                   {"@main.go"},
		"escribile a user@example.com":    nil,
		"@a.go y @b.go":                   {"@a.go", "@b.go"},
		"revisa @main.go, esta roto":      {"@main.go"},
		"(@doc.pdf) y nada mas":           {"@doc.pdf"},
		"sin referencias":                 nil,
		"@screen":                         {"@screen"},
		"@~/Descargas/spec.pdf por favor": {"@~/Descargas/spec.pdf"},
	}
	for entrada, quiero := range casos {
		got := FindRefs(entrada)
		if strings.Join(got, ",") != strings.Join(quiero, ",") {
			t.Errorf("FindRefs(%q) = %v, quiero %v", entrada, got, quiero)
		}
	}
}

func TestLaPuntuacionFinalNoEntraEnLaRuta(t *testing.T) {
	if got := FindRefs("mira @main.go."); len(got) != 1 || got[0] != "@main.go" {
		t.Errorf("got %v", got)
	}
}

func TestUnArchivoDeTextoSeAdjunta(t *testing.T) {
	r := resolverEn(t, map[string]string{"main.go": "package main\n"})
	atts, problemas := r.Expand("mira @main.go")
	if len(problemas) != 0 {
		t.Fatalf("problemas: %v", problemas)
	}
	if len(atts) != 1 || atts[0].Kind != KindText {
		t.Fatalf("atts = %+v", atts)
	}
	if !strings.Contains(atts[0].Text, "package main") {
		t.Errorf("no trajo el contenido: %q", atts[0].Text)
	}
}

func TestUnaReferenciaRotaSeInformaYNoFrenaLasDemas(t *testing.T) {
	r := resolverEn(t, map[string]string{"existe.go": "x\n"})
	atts, problemas := r.Expand("compara @existe.go con @fantasma.go")

	if len(atts) != 1 {
		t.Errorf("atts = %d, quiero 1 (la que si existe)", len(atts))
	}
	if len(problemas) != 1 {
		t.Fatalf("problemas = %d, quiero 1", len(problemas))
	}
	if !strings.Contains(problemas[0].Error(), "fantasma.go") {
		t.Errorf("el problema no dice cual fallo: %v", problemas[0])
	}
}

func TestLaMismaReferenciaDosVecesSeAdjuntaUnaSola(t *testing.T) {
	r := resolverEn(t, map[string]string{"x.go": "contenido\n"})
	atts, _ := r.Expand("@x.go y de nuevo @x.go")
	if len(atts) != 1 {
		t.Errorf("adjunto %d veces el mismo archivo", len(atts))
	}
}

func TestElCanalHumanoSaleDelWorkspaceAProposito(t *testing.T) {
	// Escribir la ruta ES la autorizacion. El agente, en cambio, sigue
	// confinado: eso lo verifica patch.SafeRelPath.
	afuera := t.TempDir()
	externo := filepath.Join(afuera, "ajeno.txt")
	if err := os.WriteFile(externo, []byte("contenido externo\n"), 0644); err != nil {
		t.Fatal(err)
	}
	r := resolverEn(t, nil)

	atts, problemas := r.Expand("mira @" + externo)
	if len(problemas) != 0 {
		t.Fatalf("rechazo una ruta que el humano escribio: %v", problemas)
	}
	if len(atts) != 1 || !strings.Contains(atts[0].Text, "contenido externo") {
		t.Errorf("atts = %+v", atts)
	}
}

func TestUnDirectorioNoEsUnAdjunto(t *testing.T) {
	r := resolverEn(t, map[string]string{"sub/x.go": "x"})
	_, problemas := r.Expand("@sub")
	if len(problemas) != 1 {
		t.Fatalf("acepto un directorio: %v", problemas)
	}
}

func TestUnTextoEnormeSeAcota(t *testing.T) {
	// Viaja en cada request de cada ronda.
	r := resolverEn(t, map[string]string{"grande.txt": strings.Repeat("linea larguisima\n", 20000)})
	atts, _ := r.Expand("@grande.txt")
	if len(atts) != 1 {
		t.Fatal("no adjunto")
	}
	if len(atts[0].Text) > MaxTextBytes+100 {
		t.Errorf("no acoto: %d bytes", len(atts[0].Text))
	}
	if !strings.Contains(atts[0].Text, "truncated") {
		t.Error("trunco sin avisar")
	}
}

func TestRenderMarcaDeDondeSaleCadaCosa(t *testing.T) {
	out := Render([]Attachment{
		{Ref: "@a.go", Kind: KindText, Source: "/ruta/a.go", Text: "contenido"},
		{Ref: "@screen", Kind: KindImage, Source: "screen capture"},
	})
	for _, q := range []string{"@a.go", "/ruta/a.go", "contenido", "@screen", "screen capture", "image"} {
		if !strings.Contains(out, q) {
			t.Errorf("falta %q en:\n%s", q, out)
		}
	}
}

func TestSinLaHerramientaElErrorDiceCualFaltaYComoVerlo(t *testing.T) {
	// Sin pdftotext, el fallo tiene que nombrar el paquete, no solo fallar.
	r := &Resolver{Workspace: t.TempDir(), Host: host.Report{}} // ninguna herramienta
	pdf := filepath.Join(r.Workspace, "doc.pdf")
	if err := os.WriteFile(pdf, []byte("%PDF-1.4\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, problemas := r.Expand("@doc.pdf")
	if len(problemas) != 1 {
		t.Fatalf("problemas = %v", problemas)
	}
	msg := problemas[0].Error()
	if !strings.Contains(msg, "poppler-utils") || !strings.Contains(msg, "doctor") {
		t.Errorf("el error no dice que instalar ni como verlo: %v", msg)
	}

	_, problemas = r.Expand("@screen")
	if len(problemas) != 1 || !strings.Contains(problemas[0].Error(), "grim") {
		t.Errorf("@screen sin grim: %v", problemas)
	}
}

func TestFreezeDejaEnPazLoQueNoEsVolatil(t *testing.T) {
	r := resolverEn(t, map[string]string{"x.go": "contenido\n"})
	dir := filepath.Join(r.Workspace, "adjuntos")

	texto, problemas := r.Freeze(dir, "revisa @x.go por favor")
	if len(problemas) != 0 {
		t.Fatalf("problemas: %v", problemas)
	}
	if texto != "revisa @x.go por favor" {
		t.Errorf("reescribio una referencia a un archivo: %q", texto)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("creo el directorio de adjuntos sin tener nada que guardar")
	}
}

func TestFreezeGuardaLoVolatilYReescribeElTexto(t *testing.T) {
	// El motivo del rechazo lo lee el agente en la corrida siguiente, cuando
	// la pantalla ya muestra otra cosa.
	r := resolverEn(t, nil)
	dir := filepath.Join(r.Workspace, "adjuntos")

	// Un resolvedor sin herramientas no puede congelar: eso se informa.
	r.Host = host.Report{}
	texto, problemas := r.Freeze(dir, "esto esta mal, mira @screen")
	if len(problemas) != 1 {
		t.Fatalf("problemas = %v", problemas)
	}
	if texto != "esto esta mal, mira @screen" {
		t.Errorf("reescribio el texto pese a no poder congelar: %q", texto)
	}
}

func TestVolatile(t *testing.T) {
	volatiles := []string{"@screen", "@screen:select", "@clipboard"}
	estables := []string{"@main.go", "@doc.pdf", "@~/algo.png", "@screenshot.png"}
	for _, v := range volatiles {
		if !Volatile(v) {
			t.Errorf("%s deberia ser volatil", v)
		}
	}
	for _, e := range estables {
		if Volatile(e) {
			t.Errorf("%s NO deberia ser volatil", e)
		}
	}
}

func TestFreezeDeTextoDelPortapapelesProduceUnArchivoLegible(t *testing.T) {
	// Simula el congelado guardando un adjunto de texto directamente.
	r := resolverEn(t, nil)
	dir := filepath.Join(r.Workspace, "adjuntos")

	ruta, err := r.save(dir, "@clipboard", Attachment{
		Kind: KindText, Source: "clipboard", Text: "lo que estaba copiado",
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(ruta)
	if err != nil {
		t.Fatalf("no se puede leer lo guardado: %v", err)
	}
	if string(data) != "lo que estaba copiado" {
		t.Errorf("contenido = %q", string(data))
	}
	if !strings.HasSuffix(ruta, ".txt") {
		t.Errorf("extension = %q, quiero .txt", ruta)
	}

	// Y una vez congelado, se resuelve como cualquier archivo.
	atts, problemas := r.Expand("mira @" + ruta)
	if len(problemas) != 0 || len(atts) != 1 {
		t.Fatalf("no se pudo releer el congelado: %v / %+v", problemas, atts)
	}
	if !strings.Contains(atts[0].Text, "lo que estaba copiado") {
		t.Errorf("el texto releido = %q", atts[0].Text)
	}
}
