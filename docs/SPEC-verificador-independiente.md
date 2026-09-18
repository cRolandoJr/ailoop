# SPEC — Verificador independiente

**Estado:** propuesto · **Fecha:** 2026-09-17 · **Protocolo:** AI_LOOP §13, §19, §20

## 1. El problema

Hoy la fase VERIFICATION concede DONE con `verify.Run`: corre los comandos que el
proyecto declaró y mira si dieron cero. Eso responde *"¿compila y pasan los
tests?"*, no *"¿esto hace lo que la Spec dice?"*.

El propio protocolo lo separa (§13): **que todos los tests pasen no implica PASS**.
Y hay una segunda cosa: el mismo instrumento corre dos veces sobre el mismo código
—una como validación local del implementador, otra como gate de DONE—, así que la
independencia del §19.1 hoy es nominal.

Medido el 17-sep sobre tres corridas de DISCOVERY con el mismo modelo y la misma
tarea: el agente **opera** bien (usa herramientas, se apoya en lo que lee) y
**obedece** un rechazo humano puntual, pero el juicio de sus artefactos es flojo y
**varía mucho entre corridas** — la primera produjo un struct con los campos
reales del endpoint, la tercera uno plano y peor. Ningún guard mecánico detecta
prosa plausible.

## 2. Qué existe ya, y no hay que construir

Dos mitades construidas que nadie conectó:

- **El turno de agente de VERIFICATION ya corre.** `propose()` se ejecuta en todas
  las fases; en VERIFICATION su salida termina en `AppendNote("[Verification
  complete]")` y se descarta.
- **El veredicto mecánico ya existe**: `l.Verify(ctx)` en el gate de DONE.

Y tres garantías que ya están cableadas:

- **El verificador no puede escribir**: `recordApproved` en `PhaseVerification` ni
  consulta los bloques. Es el §19.4 en código, desde antes de este spec.
- **Los permisos ya son los correctos**: VERIFICATION tiene lectura, búsqueda,
  `research.ask`, los servidores MCP y `cmd.run` —los comandos que el proyecto
  declaró, nunca shell libre—; no tiene escritura ni navegación de símbolos.
- **El diff sale gratis**: `patch.Backup` guarda el estado pre-loop y nunca lo
  pisa, así que `diff(backup, workspace)` es exactamente lo que cambió este work
  item, sin depender de git ni de que el usuario haya commiteado.

## 3. Decisiones tomadas

| # | Decisión | Motivo |
|---|---|---|
| D-1 | Salida **estructurada**, no prosa parseada | el crítico actual convierte un JSON malformado en rechazo; acá eso sería un veredicto inventado |
| D-2 | El veredicto **no pasa por revisión** | un veredicto es evidencia, no una propuesta que se apruebe |
| D-3 | Verifica el **modelo local** | decisión del usuario; el §4 dice cómo se compensa |
| D-4 | `verify.Run` pasa de **veredicto a evidencia** | defensa en capas del §18.14: guard mecánico *y* verificación independiente, no uno en lugar del otro |

## 4. El principio: trato asimétrico

Un `FAIL` equivocado cuesta un reintento: recuperable y barato.
Un `PASS` equivocado **concede DONE**: caro y silencioso.

Por eso no se exige lo mismo:

- para **FAIL** alcanza un hallazgo con su cita;
- para **PASS** hay que mapear **cada criterio de aceptación a evidencia
  concreta**. Si no se puede, el resultado es `UNKNOWN`.

Esto es lo que vuelve usable un juez que sabemos flojo: **se le permite acusar, no
absolver a la ligera.** Es el §19.5 llevado al diseño — *UNKNOWN is not PASS*.

## 5. Contrato del agente

**Recibe:** Spec, Design y Plan activos; el diff del work item
(`diff(backupRoot, workspace)`); la salida de `verify.Run`; Project Context.

Los **criterios de aceptación** son los de la Spec activa. Si la Spec no los
declara de forma verificable, el verificador no puede mapearlos, y eso es
`UNKNOWN` — no `PASS`.

**Produce:** un veredicto estructurado con estado y hallazgos.

```
estado:     PASS | FAIL | BLOCKED | UNKNOWN
hallazgos:  [ { severidad, afirmacion, cita } ]
cobertura:  [ { criterio, evidencia } ]     // obligatorio para PASS
```

**MUST**
- citar `archivo:línea` en cada hallazgo;
- mapear cada criterio de aceptación a evidencia para emitir `PASS`;
- distinguir lo que verificó de lo que supone.

**MUST NOT**
- modificar la implementación (ya impedido mecánicamente);
- emitir `PASS` porque los tests pasen;
- convertir `UNKNOWN` en `PASS`.

## 6. Ruteo de los cuatro estados

| Estado | Significa | A dónde va |
|---|---|---|
| `PASS` | evidencia suficiente para cada criterio | DONE |
| `FAIL` | defecto demostrado y citado | el auto-fix que ya existe (`rollbackToImplementation`) |
| `BLOCKED` | falta una precondición | **al humano** |
| `UNKNOWN` | la evidencia no alcanza | **al humano** |

Hoy el auto-fix dispara con cualquier `!report.Passed`. Con cuatro estados sólo lo
alimenta `FAIL`: reintentar contra un `BLOCKED` gasta rondas reparando algo que no
está roto.

## 7. Cómo se compensa un juez débil

**7.1 Primero la máquina, y sólo entonces el agente.**
El verificador no decide si los tests pasan —eso lo decide `verify.Run`— sino si
el cambio responde a la Spec. Y se lo llama **sólo cuando el verde mecánico ya
está**: con los checks en rojo hay `FAIL` sin gastar una llamada. Además ordena el
costo, igual que el ensayo en seco previo a la revisión.

Si el proyecto **no declaró checks** (`ErrNoChecks`), no hay verde mecánico que
preceda al agente y el resultado es `BLOCKED`: falta una precondición para poder
verificar. No es `PASS` por ausencia de pruebas.

**7.2 Cada cita se verifica sola.**
`checkCitations` ya rebota una cita a un archivo inexistente antes de que la
respuesta cuente. Hoy **no chequea los números de línea**, y el código dice que es
deliberado (*"to keep it fast"*). Para este agente hay que cerrarlo: una cita a la
línea 900 de un archivo de 40 es exactamente la alucinación que necesitamos
atrapar mecánicamente.

**7.3 Fail-closed en la ambigüedad.**
Si la salida no parsea, o falta la cobertura de un criterio, el resultado es
`UNKNOWN`. Nunca `PASS`, nunca un veredicto inventado a partir de texto suelto.

## 8. Fuera de alcance

- **`HUMAN_ACCEPTANCE`.** Con esto, `PASS → DONE` lo conceden dos gates —verde
  mecánico ∧ `PASS` del agente— **sin el usuario**. El §20.2 pide
  `PASS → HUMAN_ACCEPTANCE` y esa fase ailoop no la tiene. Es un hueco anotado, no
  un olvido. *Gatillo para volver: el primer DONE que sorprenda al usuario.*
- **Verificar con otro proveedor.** El §15 lo permitiría y sería el primer lugar
  donde dos modelos conviven en una corrida. Diferido por D-3.

## 9. Cómo se demuestra que funciona

| Propiedad | Evidencia |
|---|---|
| Un veredicto malformado da `UNKNOWN` | unit: salida basura → `UNKNOWN`, nunca `PASS` |
| `PASS` sin cobertura completa es imposible | unit: criterios sin evidencia → `UNKNOWN` |
| `FAIL` alimenta el auto-fix; `BLOCKED`/`UNKNOWN` no | unit sobre el ruteo, con los cuatro estados |
| El agente no puede escribir | ya cubierto por el guard de `recordApproved` |
| Una cita a una línea inexistente rebota | unit sobre `checkCitations` extendido |
| El verificador no corre con los checks en rojo | unit: `verify.Run` rojo → `FAIL` y cero llamadas al modelo |

El control positivo de toda la tanda: un cambio que **sí** cumple la Spec tiene que
llegar a `PASS`, o el diseño sólo sabe decir que no.
