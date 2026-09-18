package repl

import "testing"

// Escribir "/" solo es preguntar que hay. Hoy se lee como un comando de
// nombre vacio y contesta "Unknown command", que es el peor final posible
// para alguien que estaba explorando.
func TestBarraSolaPideAyuda(t *testing.T) {
	got := Interpret("/", false)
	if got.Kind != KindCommand || got.Text != "help" {
		t.Fatalf("Interpret(\"/\") = %+v, quiero el comando help", got)
	}
}

// El caso que vivio Rolando: /status sin trabajo empezado delegaba en el
// comando de un tiro, que contesta "Run 'ailoop start' first" - un consejo
// escrito para la terminal, dado a alguien que ya esta en la sesion, donde
// escribir la tarea ES empezarla.
func TestUnComandoQueNecesitaTrabajoSeAtajaAntesDeDelegar(t *testing.T) {
	for _, n := range []string{"status", "cost", "decisions", "history", "undo"} {
		if got := Interpret("/"+n, false); got.Kind != KindNeedsWork {
			t.Errorf("Interpret(\"/%s\", sin trabajo) = %+v, quiero KindNeedsWork", n, got)
		}
	}
}

// Control positivo: los que no necesitan estado tienen que seguir pasando, o
// el atajo de arriba estaria apagando comandos que funcionan. /verify sin
// loop anduvo en la sesion real.
func TestUnComandoQueNoNecesitaTrabajoPasaIgual(t *testing.T) {
	for _, n := range []string{"verify", "doctor", "capabilities", "mcp"} {
		if got := Interpret("/"+n, false); got.Kind != KindCommand {
			t.Errorf("Interpret(\"/%s\", sin trabajo) = %+v, quiero KindCommand", n, got)
		}
	}
}

func TestConTrabajoEmpezadoPasanTodos(t *testing.T) {
	for _, n := range Names() {
		if n == "exit" || n == "help" {
			continue
		}
		if got := Interpret("/"+n, true); got.Kind != KindCommand {
			t.Errorf("Interpret(\"/%s\", con trabajo) = %+v, quiero KindCommand", n, got)
		}
	}
}

// Un /help que solo lista nombres obliga a probar cada uno para saber que
// hace. La descripcion es la diferencia entre una lista y una ayuda.
func TestCadaComandoTraeDescripcion(t *testing.T) {
	cat := Catalog()
	if len(cat) == 0 {
		t.Fatal("el catalogo esta vacio")
	}
	for _, c := range cat {
		if c.Name == "" || c.Desc == "" {
			t.Errorf("%+v: falta nombre o descripcion", c)
		}
	}
}

// El catalogo es lo que se muestra, asi que tiene que incluir lo que la
// sesion acepta de verdad, exit incluido.
func TestElCatalogoIncluyeExit(t *testing.T) {
	for _, c := range Catalog() {
		if c.Name == "exit" {
			return
		}
	}
	t.Error("el catalogo no menciona exit")
}
