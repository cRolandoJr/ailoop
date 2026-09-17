package agents

import (
	"strings"
	"testing"

	"github.com/cRolandoJr/ailoop/internal/llm"
)

func observacion(ronda int, cuerpo string) llm.Message {
	return llm.Message{
		Role:    "user",
		Content: "Results of your tool requests:\n\n<<RESULT fs.read: internal/convert/convert.go>>\n" + cuerpo + "\n<<END RESULT>>",
		Round:   ronda,
	}
}

func relleno(n int) string { return strings.Repeat("linea de codigo cualquiera\n", n) }

func conversacionLarga(rondaActual int) []llm.Message {
	msgs := []llm.Message{
		{Role: "system", Content: relleno(200), Round: -1},
		{Role: "user", Content: "hace la tarea", Round: -1},
	}
	for r := 0; r <= rondaActual; r++ {
		msgs = append(msgs,
			llm.Message{Role: "assistant", Content: "pido un archivo", Round: r},
			observacion(r, relleno(350)),
		)
	}
	return msgs
}

func TestNoSeEnmascaraDebajoDelUmbral(t *testing.T) {
	// Reescribir mensajes viejos invalida el prefijo cacheado. En una
	// conversacion chica eso cuesta mas de lo que ahorra.
	msgs := []llm.Message{
		{Role: "system", Content: "corto", Round: -1},
		observacion(0, relleno(5)),
		observacion(5, relleno(5)),
	}
	if saved := maskOldObservations(msgs, 6); saved != 0 {
		t.Errorf("enmascaro con una conversacion chica: ahorro %d", saved)
	}
}

func TestSeEnmascaraLoViejoYSeConservaLoReciente(t *testing.T) {
	msgs := conversacionLarga(6)
	if n := llm.EstimateMessages(msgs); n < MaskThresholdTokens {
		t.Fatalf("el caso de prueba no supera el umbral (%d < %d): no probaria nada",
			n, MaskThresholdTokens)
	}
	antes := llm.EstimateMessages(msgs)

	saved := maskOldObservations(msgs, 6)
	if saved <= 0 {
		t.Fatalf("no ahorro nada en una conversacion larga")
	}
	despues := llm.EstimateMessages(msgs)
	if despues >= antes {
		t.Errorf("el contexto no se achico: %d -> %d", antes, despues)
	}

	// La observacion de la ronda 6 (la actual) tiene que seguir entera.
	ultima := msgs[len(msgs)-1]
	if strings.Contains(ultima.Content, "elided") {
		t.Errorf("enmascaro la observacion mas reciente")
	}
	// Una de las viejas tiene que estar elidida y decir de que era.
	var hayElidida bool
	for _, m := range msgs {
		if strings.Contains(m.Content, "elided") {
			hayElidida = true
			if !strings.Contains(m.Content, "convert.go") {
				t.Errorf("la referencia no dice de que era:\n%s", m.Content)
			}
		}
	}
	if !hayElidida {
		t.Errorf("no elidio ninguna observacion vieja")
	}
}

func TestNuncaSeEnmascaraUnaObservacionConError(t *testing.T) {
	// Esconder el error rompe el ciclo que lo esta diagnosticando.
	msgs := conversacionLarga(6)
	msgs[3].Content = "Results of your tool requests:\n\n<<RESULT cmd.run: test>>\n" +
		relleno(350) + "\nFAILED (exit 1)\nerror: undefined symbol\n<<END RESULT>>"

	maskOldObservations(msgs, 6)
	if strings.Contains(msgs[3].Content, "elided") {
		t.Errorf("enmascaro una observacion con un error adentro")
	}
	if !strings.Contains(msgs[3].Content, "undefined symbol") {
		t.Errorf("perdio el detalle del error")
	}
}

func TestNoSeTocanLosMensajesFueraDelLoop(t *testing.T) {
	msgs := conversacionLarga(6)
	sistema := msgs[0].Content
	tarea := msgs[1].Content

	maskOldObservations(msgs, 6)
	if msgs[0].Content != sistema {
		t.Errorf("modifico el system prompt, que es el prefijo cacheado")
	}
	if msgs[1].Content != tarea {
		t.Errorf("modifico el mensaje de la tarea")
	}
}

func TestUnaObservacionCortaNoSeToca(t *testing.T) {
	msgs := conversacionLarga(6)
	msgs[3] = observacion(0, "dos lineas\nnada mas")
	maskOldObservations(msgs, 6)
	if strings.Contains(msgs[3].Content, "elided") {
		t.Errorf("enmascaro algo mas chico que el propio marcador")
	}
}
