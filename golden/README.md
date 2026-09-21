# Casos golden

Cada archivo `.json` es un caso: una tarea cuya respuesta se conoce de antemano y
los checks que el resultado del circuito tiene que cumplir.

Viven acá y no en `.ailoop/` porque `.ailoop/` es estado de runtime y está
gitignoreado: un caso decide un veredicto, así que tiene que poder revisarse en un
diff. Las corridas sí son estado y van a `.ailoop/golden-runs.jsonl`.

```json
{
  "slug": "identificador-unico",
  "title": "qué protege este caso",
  "phase": "DISCOVERY | DESIGN | PLAN | IMPLEMENTATION | VERIFICATION",
  "task": "lo que se le pide al agente",
  "checks": [ { "contains": "..." } ],
  "disabled": false
}
```

Un check afirma **exactamente una** de `contains`, `not_contains` o `regex`, y lo
interpreta el código, nunca un modelo: un juez LLM mete en el instrumento la misma
varianza que el instrumento existe para detectar (DR-005).

Fallan al CARGAR, no a mitad de la corrida: un caso sin checks, un check que no
afirma nada, un check con dos afirmaciones, una regex que no compila y dos casos
con el mismo slug. Una suite vacía **no es verde**.

Un caso que el circuito deja de atrapar es un hallazgo sobre el circuito, igual que
un mutante que sobrevive invalida el test. Se ajusta el prompt o la fase, no el caso.

Correr: `ailoop golden "nota opcional"`. Gasta llamadas reales al modelo.
