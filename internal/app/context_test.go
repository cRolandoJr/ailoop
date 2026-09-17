package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loopEn(t *testing.T, archivos map[string]string) *Loop {
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
	return NewLoop(dir, nil, nil, nil)
}

func TestPorDefectoSeNombranLosArchivosNoSePegan(t *testing.T) {
	// Pegar cinco archivos en cada request paga por los cuatro que nadie abrio.
	l := loopEn(t, map[string]string{"grande.go": strings.Repeat("contenido secreto\n", 200)})

	pc, err := l.BuildProjectContext([]string{"grande.go"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pc.Text, "contenido secreto") {
		t.Error("pego el archivo entero sin que se lo pidieran")
	}
	if !strings.Contains(pc.Text, "grande.go") {
		t.Error("no nombro el archivo")
	}
	if !strings.Contains(pc.Text, "fs.read") {
		t.Error("no le dijo al agente como leerlo")
	}
}

func TestConInlineSiSePega(t *testing.T) {
	l := loopEn(t, map[string]string{"chico.go": "package x\n"})
	pc, err := l.BuildProjectContext([]string{"chico.go"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pc.Text, "package x") {
		t.Error("con --inline no pego el contenido")
	}
}

func TestUnArchivoQueNoExisteSeReportaYNoRompe(t *testing.T) {
	l := loopEn(t, map[string]string{"existe.go": "x"})
	pc, err := l.BuildProjectContext([]string{"existe.go", "fantasma.go"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(pc.Named) != 1 || pc.Named[0] != "existe.go" {
		t.Errorf("Named = %v", pc.Named)
	}
	if len(pc.Missing) != 1 || pc.Missing[0] != "fantasma.go" {
		t.Errorf("Missing = %v", pc.Missing)
	}
}

func TestLasConvencionesDelRepoLleganAlAgente(t *testing.T) {
	// Un agente que nunca ve las reglas del repo las viola sin enterarse.
	l := loopEn(t, map[string]string{
		"CLAUDE.md": "En este repo los identificadores van en espanol.",
	})
	pc, err := l.BuildProjectContext(nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pc.Text, "identificadores van en espanol") {
		t.Errorf("no inyecto las convenciones:\n%s", pc.Text)
	}
	if !strings.Contains(pc.Text, "CLAUDE.md") {
		t.Error("no dice de donde salieron las convenciones")
	}
}

func TestSeEligeElArchivoDeConvencionesMasEspecifico(t *testing.T) {
	l := loopEn(t, map[string]string{
		"CLAUDE.md":       "reglas especificas",
		"CONTRIBUTING.md": "guia generica",
	})
	pc, _ := l.BuildProjectContext(nil, false)
	if !strings.Contains(pc.Text, "reglas especificas") {
		t.Error("no priorizo CLAUDE.md")
	}
	if strings.Contains(pc.Text, "guia generica") {
		t.Error("inyecto los dos archivos")
	}
}

func TestLasConvencionesEnormesSeAcotan(t *testing.T) {
	// Viajan en cada request de cada ronda.
	l := loopEn(t, map[string]string{
		"CLAUDE.md": strings.Repeat("regla muy larga\n", 5000),
	})
	pc, _ := l.BuildProjectContext(nil, false)
	if len(pc.Text) > maxConventionBytes+2000 {
		t.Errorf("no acoto: %d bytes", len(pc.Text))
	}
	if !strings.Contains(pc.Text, "truncated") {
		t.Error("truncó sin avisar")
	}
}

func TestSinConvencionesNoInventaNada(t *testing.T) {
	l := loopEn(t, nil)
	pc, _ := l.BuildProjectContext(nil, false)
	if strings.Contains(pc.Text, "PROJECT_CONVENTIONS") {
		t.Errorf("genero un bloque de convenciones vacio:\n%s", pc.Text)
	}
}
