package patch

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func workspaceCon(t *testing.T, archivos map[string]string) string {
	t.Helper()
	ws := t.TempDir()
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

func TestNoSePuedeParchearUnArchivoQueElAgenteNoLeyo(t *testing.T) {
	// El bloque de busqueda de un archivo que nunca se abrio esta inventado.
	ws := workspaceCon(t, map[string]string{"main.go": "package main\n"})
	prop := "<<<<\nmain.go\n====\npackage main\n====\npackage otro\n>>>>\n"

	err := Apply(ws, prop, NewSeen()) // leyo nada
	if !errors.Is(err, ErrUnread) {
		t.Fatalf("err = %v, quiero ErrUnread", err)
	}

	// Y el archivo no se toco.
	data, _ := os.ReadFile(filepath.Join(ws, "main.go"))
	if string(data) != "package main\n" {
		t.Errorf("modifico el archivo pese al rechazo: %q", string(data))
	}
}

func TestHabiendoLeidoSiSePuedeParchear(t *testing.T) {
	ws := workspaceCon(t, map[string]string{"main.go": "package main\n"})
	prop := "<<<<\nmain.go\n====\npackage main\n====\npackage otro\n>>>>\n"

	if err := Apply(ws, prop, NewSeen("main.go")); err != nil {
		t.Fatalf("rechazo un parche legitimo: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(ws, "main.go"))
	if string(data) != "package otro\n" {
		t.Errorf("no aplico: %q", string(data))
	}
}

func TestUnSoloArchivoSinLeerFrenaTodoElParche(t *testing.T) {
	// Un parche que aterriza a medias es peor que uno que no aterriza.
	ws := workspaceCon(t, map[string]string{
		"leido.go":    "contenido A\n",
		"no_leido.go": "contenido B\n",
	})
	prop := "<<<<\nleido.go\n====\ncontenido A\n====\nPARCHEADO\n>>>>\n" +
		"<<<<\nno_leido.go\n====\ncontenido B\n====\nPARCHEADO\n>>>>\n"

	err := Apply(ws, prop, NewSeen("leido.go"))
	if !errors.Is(err, ErrUnread) {
		t.Fatalf("err = %v, quiero ErrUnread", err)
	}
	data, _ := os.ReadFile(filepath.Join(ws, "leido.go"))
	if strings.Contains(string(data), "PARCHEADO") {
		t.Error("aplico el bloque valido pese a rechazar el otro")
	}
}

func TestElMensajeLeDiceAlAgenteQueHacer(t *testing.T) {
	// Un rechazo que no explica como seguir hace que el agente reintente igual.
	ws := workspaceCon(t, map[string]string{"x.go": "a\n"})
	err := Apply(ws, "<<<<\nx.go\n====\na\n====\nb\n>>>>\n", NewSeen("otro.go"))
	if err == nil {
		t.Fatal("no rechazo")
	}
	msg := err.Error()
	if !strings.Contains(msg, "fs.read") {
		t.Errorf("no le dice como arreglarlo: %q", msg)
	}
	if !strings.Contains(msg, "otro.go") {
		t.Errorf("no le dice que si leyo: %q", msg)
	}
}

func TestSeenNilDesactivaElChequeo(t *testing.T) {
	// Un llamador sin agente de por medio no tiene nada que verificar.
	ws := workspaceCon(t, map[string]string{"x.go": "a\n"})
	if err := Apply(ws, "<<<<\nx.go\n====\na\n====\nb\n>>>>\n", nil); err != nil {
		t.Errorf("el opt-out no funciono: %v", err)
	}
}

func TestSeenNormalizaLasRutas(t *testing.T) {
	// "./a/b.go" y "a/b.go" son el mismo archivo.
	s := NewSeen("./internal/x.go")
	if !s.Has("internal/x.go") {
		t.Errorf("no normalizo: %v", s.Paths())
	}
}

func TestSeenVacioNoEsLoMismoQueNil(t *testing.T) {
	// nil = "no chequees". Vacio = "el agente no leyo nada", y entonces
	// ningun parche es legitimo. Confundirlos apagaria el chequeo justo
	// cuando mas hace falta.
	ws := workspaceCon(t, map[string]string{"x.go": "a\n"})
	prop := "<<<<\nx.go\n====\na\n====\nb\n>>>>\n"

	if err := Apply(ws, prop, nil); err != nil {
		t.Errorf("nil deberia desactivar el chequeo: %v", err)
	}
	// restaurar para el segundo caso
	os.WriteFile(filepath.Join(ws, "x.go"), []byte("a\n"), 0644)

	if err := Apply(ws, prop, NewSeen()); !errors.Is(err, ErrUnread) {
		t.Errorf("un Seen vacio dejo pasar el parche: %v", err)
	}
}
