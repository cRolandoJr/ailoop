package patch

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeRelPathRechazaLoQueEscapa(t *testing.T) {
	ws := t.TempDir()
	malos := []string{
		"/etc/passwd",
		"../afuera.go",
		"../../../../etc/shadow",
		"a/../../fuera.txt",
		"",
		"   ",
	}
	for _, m := range malos {
		if got, err := SafeRelPath(ws, m); err == nil {
			t.Errorf("SafeRelPath(%q) = %q sin error, quiero rechazo", m, got)
		}
	}
}

func TestSafeRelPathAceptaLoDeAdentro(t *testing.T) {
	ws := t.TempDir()
	buenos := map[string]string{
		"main.go":                 "main.go",
		"internal/patch/apply.go": "internal/patch/apply.go",
		"./internal/a/../b/x.go":  "internal/b/x.go",
		"carpeta/sub/archivo.txt": "carpeta/sub/archivo.txt",
	}
	for in, quiero := range buenos {
		got, err := SafeRelPath(ws, in)
		if err != nil {
			t.Errorf("SafeRelPath(%q) devolvio error: %v", in, err)
			continue
		}
		if got != quiero {
			t.Errorf("SafeRelPath(%q) = %q, quiero %q", in, got, quiero)
		}
	}
}

func TestApplyRechazaTodoSiUnPathEsMalo(t *testing.T) {
	ws := t.TempDir()
	bueno := filepath.Join(ws, "ok.txt")
	if err := os.WriteFile(bueno, []byte("hola\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// El primer bloque es valido, el segundo escapa. No debe aplicarse NINGUNO.
	prop := "<<<<\nok.txt\n====\nhola\n====\nchau\n>>>>\n" +
		"<<<<\n../fuera.txt\n====\na\n====\nb\n>>>>\n"

	err := Apply(ws, prop, nil)
	if !errors.Is(err, ErrOutsideWorkspace) {
		t.Fatalf("err = %v, quiero ErrOutsideWorkspace", err)
	}

	data, _ := os.ReadFile(bueno)
	if string(data) != "hola\n" {
		t.Errorf("el archivo valido fue modificado pese al rechazo: %q", string(data))
	}
}

func TestApplyRechazaBusquedaAmbigua(t *testing.T) {
	// Reemplazar "la primera" de varias coincidencias identicas parchea un
	// lugar que nadie eligio.
	ws := t.TempDir()
	f := filepath.Join(ws, "dup.go")
	if err := os.WriteFile(f, []byte("x := 1\ny := 2\nx := 1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err := Apply(ws, "<<<<\ndup.go\n====\nx := 1\n====\nx := 9\n>>>>\n", nil)
	if err == nil {
		t.Fatalf("aplico un patch ambiguo sin avisar")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("err = %v, quiero que mencione la ambiguedad", err)
	}
}

func TestParseBlocksAvisaCuandoElBloqueEstaRoto(t *testing.T) {
	// Antes, un bloque sin cerrar se descartaba en silencio: el usuario
	// aprobaba una implementacion que no se aplicaba nunca.
	if _, err := ParseBlocks("<<<<\nfile.go\n====\na\n====\nb\n"); err == nil {
		t.Errorf("un bloque sin >>>> no produjo error")
	}
	if _, err := ParseBlocks("<<<<\nfile.go\n====\nsolo dos secciones\n>>>>\n"); err == nil {
		t.Errorf("un bloque incompleto no produjo error")
	}
}

func TestCicloCompletoBackupYRestore(t *testing.T) {
	ws := t.TempDir()

	// Dos archivos con el MISMO nombre base en carpetas distintas: es el caso
	// que el backup plano se comia.
	for _, rel := range []string{"a/main.go", "b/main.go"} {
		full := filepath.Join(ws, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("original de "+rel+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	prop := "<<<<\na/main.go\n====\noriginal de a/main.go\n====\nPARCHEADO a\n>>>>\n" +
		"<<<<\nb/main.go\n====\noriginal de b/main.go\n====\nPARCHEADO b\n>>>>\n"

	if err := Apply(ws, prop, nil); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for _, rel := range []string{"a/main.go", "b/main.go"} {
		data, _ := os.ReadFile(filepath.Join(ws, rel))
		if !strings.Contains(string(data), "PARCHEADO") {
			t.Fatalf("%s no fue parcheado: %q", rel, string(data))
		}
	}

	res, err := Restore(ws)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if len(res.Failed) != 0 {
		t.Fatalf("Restore reporto fallos: %v", res.Failed)
	}
	if len(res.Restored) != 2 {
		t.Errorf("Restored = %v, quiero 2 archivos", res.Restored)
	}

	for _, rel := range []string{"a/main.go", "b/main.go"} {
		data, _ := os.ReadFile(filepath.Join(ws, rel))
		quiero := "original de " + rel + "\n"
		if string(data) != quiero {
			t.Errorf("%s = %q, quiero %q", rel, string(data), quiero)
		}
	}
}

func TestRestoreBorraLoQueElLoopCreo(t *testing.T) {
	ws := t.TempDir()

	// Un archivo que no existia: Backup lo marca como creado.
	if err := Backup(ws, "nuevo.go"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "nuevo.go"), []byte("creado por el loop\n"), 0644); err != nil {
		t.Fatal(err)
	}

	res, err := Restore(ws)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if len(res.Deleted) != 1 {
		t.Errorf("Deleted = %v, quiero 1", res.Deleted)
	}
	if _, err := os.Stat(filepath.Join(ws, "nuevo.go")); !os.IsNotExist(err) {
		t.Errorf("el archivo creado por el loop sobrevivio al rollback")
	}
}

func TestRestoreSinBackupsNoMiente(t *testing.T) {
	res, err := Restore(t.TempDir())
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if len(res.Restored) != 0 || len(res.Deleted) != 0 {
		t.Errorf("reporto trabajo que no hizo: %+v", res)
	}
}
