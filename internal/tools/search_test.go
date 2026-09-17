package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func armarArbol(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	archivos := map[string]string{
		"main.go":                  "package main\n\nfunc Normalizar(s string) string { return s }\n",
		"internal/a/util.go":       "package a\n\n// llama a Normalizar\nfunc Usa() {}\n",
		"internal/a/util_test.go":  "package a\n\nfunc TestUsa(t *testing.T) {}\n",
		"internal/b/otro.go":       "package b\n",
		"node_modules/basura/x.go": "func Normalizar() {}\n",
		".git/objects/algo":        "Normalizar\n",
		"docs/notas.md":            "hablamos de Normalizar aca\n",
	}
	for rel, body := range archivos {
		full := filepath.Join(ws, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return ws
}

func TestGlobCruzaDirectoriosConDobleAsterisco(t *testing.T) {
	ws := armarArbol(t)
	out, err := Glob(ws, "internal/**/*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, quiero := range []string{"internal/a/util.go", "internal/b/otro.go"} {
		if !strings.Contains(out, quiero) {
			t.Errorf("Glob no encontro %s:\n%s", quiero, out)
		}
	}
	if strings.Contains(out, "main.go") {
		t.Errorf("Glob devolvio algo fuera del patron:\n%s", out)
	}
}

func TestGlobYGrepIgnoranDirectoriosDeRuido(t *testing.T) {
	// node_modules y .git son grandes, generados, y nunca son la respuesta.
	ws := armarArbol(t)

	g, err := Glob(ws, "**/*.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(g, "node_modules") {
		t.Errorf("Glob entro a node_modules:\n%s", g)
	}

	r, err := Grep(ws, "Normalizar", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r, "node_modules") || strings.Contains(r, ".git") {
		t.Errorf("Grep entro a directorios de ruido:\n%s", r)
	}
}

func TestGrepDevuelveArchivoLineaYTexto(t *testing.T) {
	// file:line:texto es lo que vuelve verificable una afirmacion del agente.
	ws := armarArbol(t)
	out, err := Grep(ws, "func Normalizar", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "main.go:3:") {
		t.Errorf("Grep no devolvio archivo:linea:\n%s", out)
	}
}

func TestGrepAcotaConGlob(t *testing.T) {
	ws := armarArbol(t)
	out, err := Grep(ws, "Normalizar", "docs/**")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "docs/notas.md") {
		t.Errorf("no encontro en docs:\n%s", out)
	}
	if strings.Contains(out, "main.go") {
		t.Errorf("el glob no acoto la busqueda:\n%s", out)
	}
}

func TestGrepSinResultadosLoDiceExplicitamente(t *testing.T) {
	// "sin salida" y "no busque" son indistinguibles; por eso se enuncia.
	ws := armarArbol(t)
	out, err := Grep(ws, "EstoNoExisteEnNingunLado", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no matches") {
		t.Errorf("un resultado vacio no se enuncio: %q", out)
	}
}

func TestGrepRechazaRegexpInvalida(t *testing.T) {
	if _, err := Grep(t.TempDir(), "func (", ""); err == nil {
		t.Errorf("acepto una regexp invalida sin avisar")
	}
}

func TestSplitGrepArg(t *testing.T) {
	casos := []struct{ in, pat, glob string }{
		{"func Foo", "func Foo", ""},
		{"func Foo -- **/*.go", "func Foo", "**/*.go"},
		{"a -- b -- c", "a", "b -- c"},
	}
	for _, c := range casos {
		p, g := splitGrepArg(c.in)
		if p != c.pat || g != c.glob {
			t.Errorf("splitGrepArg(%q) = (%q,%q), quiero (%q,%q)", c.in, p, g, c.pat, c.glob)
		}
	}
}

func TestMatchPathSoportaPatronesComunes(t *testing.T) {
	casos := []struct {
		pat, path string
		quiero    bool
	}{
		{"**/*.go", "internal/a/b.go", true},
		{"*.go", "main.go", true},
		{"*.go", "internal/a.go", false},
		{"internal/**", "internal/a/b.go", true},
		{"internal/**/*_test.go", "internal/a/x_test.go", true},
		{"internal/**/*_test.go", "internal/a/x.go", false},
		{"", "cualquier/cosa", true},
	}
	for _, c := range casos {
		if got := matchPath(c.pat, c.path); got != c.quiero {
			t.Errorf("matchPath(%q, %q) = %v, quiero %v", c.pat, c.path, got, c.quiero)
		}
	}
}
