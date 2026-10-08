# Struttura della CLI

- `main.go`: entry point, stampa dell'errore e codice di uscita.
- `cli.go`: registro dei comandi, dispatch e usage generato dal registro.
- `help.go`: help generale e per comando, opzioni da FlagSet, esempi e colori automatici.
- `generate.go`, `inspect.go` e `run.go`: flag, validazione ed esecuzione del rispettivo comando.
- `project.go`: risoluzione dei target impliciti da `pfw.toml` alla root del modulo.
- `version.go`: versione da metadati Go o incorporata nei binari di release.
- `new.go`: creazione di progetti (`new`) ed elenco dei template (`templates`).

I template sono inclusi con `go:embed` in `internal/scaffold/templates`.
Le varianti HTTP dell'API sono in `internal/scaffold/routers`, con un registro
dei router e delle loro dipendenze in `scaffold.Routers()`. `new -router` seleziona
la variante; `templates` mostra le scelte disponibili.
`internal/scaffold` valida le opzioni, renderizza e formatta i file e crea
un modulo indipendente. Il README di ciascun template documenta i passi successivi.

Per aggiungere un comando, creare un file con una dichiarazione `command` e
un handler `func(context.Context, []string, io.Writer, io.Writer) error`, quindi
aggiungere una voce a `commands` in `cli.go`. Il dispatcher e `main` non cambiano.
Ogni invocazione crea il proprio `FlagSet`; stdout/stderr restano iniettabili
nei test. La dichiarazione `command` contiene anche descrizione, esempi e note.
Usare `parseCommandFlags` per gestire parsing e help uniformemente. La sintassi
fra backtick nelle descrizioni dei flag definisce il placeholder mostrato nell'help.

`pfw`, `pfw --help` e `pfw help` mostrano l'help generale. `pfw help run` e
`pfw run --help` mostrano quello del comando. L'help scrive su stdout ed esce
con successo, anche fuori da un modulo Go. Gli errori vengono riportati una sola
volta su stderr e suggeriscono il comando di help da consultare.

I colori sono automatici solo su terminale, rispettando `NO_COLOR` e `TERM=dumb`.
L'output reindirizzato rimane testo semplice. L'elenco dei comandi e dei flag è
ordinato per ottenere un output deterministico; opzioni e default derivano dai
FlagSet effettivi dei comandi.
