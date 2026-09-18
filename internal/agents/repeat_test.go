package agents

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cRolandoJr/ailoop/internal/state"
)

func ultimoMensaje(t *testing.T, f *fakeLLM, llamada int) string {
	t.Helper()
	if len(f.seen) <= llamada {
		t.Fatalf("el modelo se llamo %d veces, esperaba al menos %d", len(f.seen), llamada+1)
	}
	msgs := f.seen[llamada]
	return msgs[len(msgs)-1].Content
}

// Medido contra qwen: pidio CUATRO veces el mismo archivo inexistente, y
// ailoop le devolvio el mismo error cuatro veces sin senialarle nunca que ya
// lo habia pedido. El tope son 7 rondas, asi que repetir es gastar el unico
// recurso escaso que tiene la fase.
func TestUnPedidoRepetidoSeLeAvisaAlAgente(t *testing.T) {
	f := &fakeLLM{replies: []string{
		pedir("fs.read", "internal/moodle/moodle.go"),
		pedir("fs.read", "internal/moodle/moodle.go"),
		"listo",
	}}

	s := state.NewState("tarea")
	if _, err := RunPhase(context.Background(), PhaseInput{State: s, Client: f, Workspace: t.TempDir()}); err != nil {
		t.Fatalf("RunPhase: %v", err)
	}

	segunda := ultimoMensaje(t, f, 2)
	if !strings.Contains(strings.ToLower(segunda), "already asked") {
		t.Errorf("no le avisa que repitio el pedido:\n%s", segunda)
	}
}

// Control positivo: dos pedidos DISTINTOS no pueden marcarse como repetidos, o
// la seniial no sirve para nada.
func TestDosPedidosDistintosNoSeMarcanComoRepetidos(t *testing.T) {
	ws := t.TempDir()
	for _, n := range []string{"uno.go", "dos.go"} {
		if err := os.WriteFile(filepath.Join(ws, n), []byte("package x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	f := &fakeLLM{replies: []string{
		pedir("fs.read", "uno.go"),
		pedir("fs.read", "dos.go"),
		"listo",
	}}

	s := state.NewState("tarea")
	if _, err := RunPhase(context.Background(), PhaseInput{State: s, Client: f, Workspace: ws}); err != nil {
		t.Fatalf("RunPhase: %v", err)
	}

	segunda := ultimoMensaje(t, f, 2)
	if strings.Contains(strings.ToLower(segunda), "already asked") {
		t.Errorf("marco como repetido un pedido distinto:\n%s", segunda)
	}
}

// La ronda de corte usaba client.Generate: es la unica del lazo que no
// transmite, justo la que produce la respuesta final. La pantalla quedaba muda
// exactamente cuando hay algo que leer.
func TestLaRondaDeCorteTambienSeTransmite(t *testing.T) {
	// Un modelo que pide herramienta siempre: agota el presupuesto y fuerza
	// la ronda de corte.
	var replies []string
	for i := 0; i <= MaxToolRounds+1; i++ {
		replies = append(replies, pedir("fs.read", "no/existe.go"))
	}
	f := &fakeLLM{replies: replies}

	s := state.NewState("tarea")
	if _, err := RunPhase(context.Background(), PhaseInput{State: s, Client: f, Workspace: t.TempDir()}); err != nil {
		t.Fatalf("RunPhase: %v", err)
	}

	if f.streamed != f.calls {
		t.Errorf("%d de %d llamadas se transmitieron: la ronda de corte quedo muda", f.streamed, f.calls)
	}
}
