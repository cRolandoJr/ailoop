package app

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/cRolandoJr/ailoop/internal/config"
	"github.com/cRolandoJr/ailoop/internal/lsp"
)

// Nada cerraba el servidor de lenguaje: cada comando pesado dejaba un gopls
// vivo hasta que el proceso salia. El loop lo abre, asi que el loop lo cierra.
func TestCloseTerminaElServidorDeLenguaje(t *testing.T) {
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("no hay 'cat' para hacer de servidor")
	}
	cliente, err := lsp.Start(context.Background(), "cat")
	if err != nil {
		t.Fatal(err)
	}
	l := NewLoop(t.TempDir(), &modeloScript{}, nil, &config.Config{}, cliente)

	hecho := make(chan error, 1)
	go func() { hecho <- l.Close() }()

	select {
	case err := <-hecho:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close se colgo esperando al proceso")
	}
}

// El loop de los comandos de solo lectura no tiene servidor, y cerrarlo no
// puede ser un caso especial que el llamador tenga que recordar.
func TestCloseSinServidorNoRompe(t *testing.T) {
	l := NewLoop(t.TempDir(), &modeloScript{}, nil, &config.Config{}, nil)
	if err := l.Close(); err != nil {
		t.Fatalf("Close sin cliente: %v", err)
	}
}
