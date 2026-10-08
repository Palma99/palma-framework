# Struttura della CLI

- `main.go`: entry point, stampa dell'errore e codice di uscita.
- `cli.go`: registro dei comandi, dispatch e usage generato dal registro.
- `generate.go`, `inspect.go` e `run.go`: flag, validazione ed esecuzione del rispettivo comando.
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
nei test. L'elenco nell'usage viene ordinato per avere un output deterministico.
