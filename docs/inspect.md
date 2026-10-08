# Ispezione del grafo DI

`pfw inspect` analizza le dichiarazioni e risolve il grafo senza generare o
modificare file. Usa lo stesso frontend e resolver di `pfw generate`.

```sh
go run ./cmd/pfw inspect ./examples/httpapi/internal/bootstrap
go run ./cmd/pfw inspect -json ./examples/httpapi/internal/bootstrap
go run ./cmd/pfw inspect -full ./examples/httpapi/internal/bootstrap
go run ./cmd/pfw inspect -color=always ./examples/httpapi/internal/bootstrap
go run ./cmd/pfw inspect -env local ./examples/httpapi/internal/bootstrap
```

Con la CLI installata: `pfw inspect [packages...]`. Senza pattern usa `bootstrap`
nel `pfw.toml` alla root del modulo, con default `./internal/bootstrap`. I percorsi
configurati sono relativi alla root del modulo. I pattern espliciti prevalgono
e seguono le stesse regole Go usate da `generate`, per esempio `.` o `./...`. La discovery resta limitata ai package
selezionati nei marker `Discover` dei singoli initializer.

## Contenuto del report

Per gli initializer environment-aware viene prodotto un report per ambiente;
`-env` seleziona quale mostrare. I provider override hanno un badge nella vista
testuale e un campo `override` nel JSON.

Il report distingue ogni initializer e mostra:

- Tipo radice, parametri in ingresso e modalità con/senza cleanup.
- Costruttori registrati, output, dipendenze, file sorgente quando disponibile.
- Provenienza `manual` e/o `coconut`, moduli e scope di `AutoBind`.
- Stato `used`, `unused` o `excluded` dei provider candidati.
- Provenienza `framework_default` e campo JSON `fallback` per i provider standard
  usati quando mancano registrazioni applicative.
- Impostazione globale `autobind` dell'initializer e valore ereditato/locale dei
  moduli; gli scope riportati per provider e binding riflettono la precedenza effettiva.
- Binding manuali, automatici e `direct` (input/provider già tipizzato con
  l'interfaccia), con consumer e provider selezionato.
- Ordine di costruzione effettivo e ordine inverso dei cleanup raggiungibili.

`unused` significa registrato ma non raggiungibile dalla radice. `excluded`
significa scoperto e poi escluso senza una registrazione manuale che lo mantenga.
`exclusion_requested` nel JSON distingue un'esclusione richiesta da quella
effettiva. I moduli non utilizzati dall'initializer e i package non selezionati
non fanno parte di quel grafo.

Il cleanup order riporta i costruttori delle risorse, non un'esecuzione dei
callback. Nessun costruttore viene chiamato e nessuna configurazione applicativa
viene caricata durante l'ispezione.

## Output JSON

Il documento contiene `version: 1` e un array `initializers`. Ogni elemento
contiene `inputs`, `providers`, `modules`, `bindings`, `construction_order` e
`cleanup_order`. Nomi di package e tipi sono qualificati per evitare ambiguità.
Provider, moduli e binding sono ordinati; gli ordini di costruzione e cleanup
mantengono invece l'ordine di esecuzione del piano. A parità di sorgenti e contesto
di build il report è deterministico. I percorsi sorgente possono essere assoluti.

## Vista terminale

La vista predefinita mostra nomi brevi, un riepilogo dei provider e il piano
di costruzione, con dipendenze e provenienza sotto ogni step. I provider non
costruiti sono raccolti in una sezione separata. I suffissi dei package vengono
allungati solo quando necessario a distinguere omonimi, per esempio
`platform/http` e `adapter/http`. Un package versionato isolato come `echo/v5`
viene mostrato come `echo`; versioni diverse nello stesso report rimangono distinte.
I moduli inline hanno etichette locali `module1`, `module2`, evitando percorsi
sorgente nel nome. I letterali dei tipi non vengono abbreviati.

`-full` mantiene i riferimenti completi e mostra le posizioni sorgente dei provider.
Il JSON rimane invariato e usa sempre nomi completi.

`-color=auto` è il default: i colori vengono abilitati solo su un terminale,
rispettando `NO_COLOR` e `TERM=dumb`. Le pipe, i file e il JSON non ricevono
sequenze ANSI automaticamente. `-color=always` forza i colori nella vista
testuale; `-color=never` li disabilita. Stato e provenienza sono sempre scritti
anche come testo, così il colore non è necessario per interpretare il report.

L'ispezione funziona anche prima della prima generazione e non verifica se i
file generati sono aggiornati. Per questo usare `pfw generate -check`.
Un grafo invalido produce la stessa diagnostica di risoluzione del generatore;
la CLI scrive l'errore su stderr ed esce con codice non zero, senza un report
parziale. La verifica del codice emesso nel normale contesto di build rimane
responsabilità di `generate`.
