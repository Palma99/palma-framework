# Struttura della CLI

- `main.go`: entry point, stampa dell'errore e codice di uscita.
- `cli.go`: registro dei comandi, dispatch e usage generato dal registro.
- `generate.go`, `inspect.go` e `run.go`: flag, validazione ed esecuzione del rispettivo comando.

Per aggiungere un comando, creare un file con una dichiarazione `command` e
un handler `func(context.Context, []string, io.Writer, io.Writer) error`, quindi
aggiungere una voce a `commands` in `cli.go`. Il dispatcher e `main` non cambiano.
Ogni invocazione crea il proprio `FlagSet`; stdout/stderr restano iniettabili
nei test. L'elenco nell'usage viene ordinato per avere un output deterministico.
