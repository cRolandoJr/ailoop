package repl

import (
	"sort"
	"testing"
)

func TestSinTrabajoElTextoEsLaTarea(t *testing.T) {
	got := Interpret("arreglar el parser de fechas", false)

	if got.Kind != KindStart {
		t.Errorf("Kind = %v, quiero KindStart", got.Kind)
	}
	if got.Text != "arreglar el parser de fechas" {
		t.Errorf("Text = %q", got.Text)
	}
}

func TestConTrabajoEnCursoElTextoEsFeedback(t *testing.T) {
	// El mismo texto, otro destino. Esa es toda la regla.
	got := Interpret("le falta el caso vacio", true)

	if got.Kind != KindAdvance {
		t.Errorf("Kind = %v, quiero KindAdvance", got.Kind)
	}
	if got.Text != "le falta el caso vacio" {
		t.Errorf("Text = %q, se esperaba el feedback", got.Text)
	}
}

func TestEnterVacioAvanzaSinFeedback(t *testing.T) {
	got := Interpret("", true)

	if got.Kind != KindAdvance {
		t.Errorf("Kind = %v, quiero KindAdvance", got.Kind)
	}
	if got.Text != "" {
		t.Errorf("Text = %q: un Enter vacio no deberia inventar feedback", got.Text)
	}
}

func TestEnterVacioSinTrabajoNoHaceNada(t *testing.T) {
	// Avanzar aca le pediria al agente que trabaje sobre nada.
	if got := Interpret("", false); got.Kind != KindNothing {
		t.Errorf("Kind = %v, quiero KindNothing", got.Kind)
	}
}

func TestLaBarraEsUnComando(t *testing.T) {
	got := Interpret("/cost", true)

	if got.Kind != KindCommand {
		t.Errorf("Kind = %v, quiero KindCommand", got.Kind)
	}
	if got.Text != "cost" {
		t.Errorf("Text = %q, quiero el nombre sin la barra", got.Text)
	}
}

func TestUnComandoConservaSusArgumentos(t *testing.T) {
	got := Interpret("/mcp --schemas", true)

	if got.Text != "mcp" || got.Args != "--schemas" {
		t.Errorf("Text = %q, Args = %q", got.Text, got.Args)
	}
}

func TestUnaLineaConBarraInicialSiempreEsComando(t *testing.T) {
	// Decision de diseno, fijada aca para que se vea: la barra inicial se
	// reserva sin excepciones. Escribir una ruta al principio de una frase la
	// convierte en comando, y el precio se paga en el otro lado - el nombre
	// no esta en la tabla, asi que la sesion lo reporta en vez de mandarselo
	// al agente. Adivinar cual de los dos era habria fallado en silencio.
	got := Interpret("/tmp/smoke esta roto", true)

	if got.Kind != KindCommand {
		t.Fatalf("Kind = %v, quiero KindCommand", got.Kind)
	}
	if _, known := Command(got.Text); known {
		t.Errorf("%q no deberia ser un comando conocido", got.Text)
	}
}

func TestSalirPorCualquierNombre(t *testing.T) {
	for _, linea := range []string{"/exit", "/quit", "/EXIT"} {
		if got := Interpret(linea, true); got.Kind != KindExit {
			t.Errorf("%q: Kind = %v, quiero KindExit", linea, got.Kind)
		}
	}
}

func TestSeIgnoranLosEspaciosDeMas(t *testing.T) {
	if got := Interpret("   ", true); got.Kind != KindAdvance {
		t.Errorf("una linea de espacios deberia contar como Enter vacio, dio %v", got.Kind)
	}
	if got := Interpret("  /cost  ", true); got.Kind != KindCommand || got.Text != "cost" {
		t.Errorf("Kind = %v, Text = %q", got.Kind, got.Text)
	}
}

func TestTodoComandoConocidoExisteEnElCLI(t *testing.T) {
	// Control positivo sobre la tabla real: un nombre que run() no maneja
	// imprimiria "Unknown command" desde adentro de la sesion.
	validos := map[string]bool{
		"status": true, "cost": true, "decisions": true, "history": true,
		"undo": true, "verify": true, "doctor": true, "capabilities": true,
		"mcp": true,
	}
	for _, n := range Names() {
		if n == "exit" {
			continue
		}
		cli, known := Command(n)
		if !known {
			t.Errorf("%q se lista en Names pero Command no lo conoce", n)
			continue
		}
		if !validos[cli] {
			t.Errorf("%q mapea a %q, que no es un comando del CLI", n, cli)
		}
	}
}

func TestUnComandoDesconocidoSeReportaComoTal(t *testing.T) {
	if _, known := Command("inventado"); known {
		t.Error("un comando inexistente no deberia reportarse como conocido")
	}
}

func TestNamesNoTienteDuplicados(t *testing.T) {
	names := Names()
	sort.Strings(names)
	for i := 1; i < len(names); i++ {
		if names[i] == names[i-1] {
			t.Errorf("%q aparece dos veces en Names", names[i])
		}
	}
}

func TestNadaSalvoExitTerminaLaSesion(t *testing.T) {
	// El equivalente del defecto que tenia el menu anterior: avanzar una fase
	// expulsaba a la terminal. Aca la unica forma de salir es pedirlo, asi que
	// se barre el espacio de entradas para que ninguna otra devuelva KindExit.
	entradas := []string{
		"", "   ",
		"arreglar el parser",
		"le falta el caso vacio",
		"/status", "/cost", "/undo", "/verify", "/help",
		"/comando-que-no-existe",
		"salir", "exit", "quit", // sin barra: son texto para el agente
	}
	for _, in := range entradas {
		for _, started := range []bool{false, true} {
			if got := Interpret(in, started); got.Kind == KindExit {
				t.Errorf("Interpret(%q, started=%v) termina la sesion y no deberia", in, started)
			}
		}
	}
}
