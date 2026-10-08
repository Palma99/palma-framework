# Palma: proposta di architettura

Palma è un framework Go opinionato, ispirato all'esperienza di Symfony e Spring
Boot, con un composition root generato a build time. Questo documento è una
proposta iniziale: le convenzioni pubbliche vanno concordate con l'autore.

## Direzione concordata

Palma è focalizzato sulla costruzione di backend, in particolare API.
HTTP è il primo trasporto e l'unico previsto per la prima versione.
L'architettura deve permettere di aggiungere in seguito gRPC o altri trasporti
senza cambiare il nucleo DI e senza richiedere una riscrittura dei servizi.
Il supporto di questi trasporti futuri non fa parte dell'implementazione iniziale.

Palma non impone una struttura di cartelle, una nomenclatura o uno stile
architetturale. MVC, architettura esagonale, organizzazione per funzionalità
e applicazioni piccole in un solo package devono essere ugualmente supportati.
Le opinioni del framework riguardano composizione, DI e lifecycle, non il
layout del codice applicativo.

Anche la libreria HTTP è una scelta dell'applicazione: `net/http`, Echo, Chi,
Gin o una soluzione personalizzata. Il nucleo Palma non deve dipendere da uno
specifico router o framework HTTP. Le integrazioni descritte sotto sono requisiti
progettuali, non funzionalità già implementate.

## Contratto fondamentale

- I servizi sono normali tipi Go. Le dipendenze entrano nei costruttori.
- Il generatore analizza codice e tipi, risolve il grafo e scrive codice Go.
- Il programma compilato chiama direttamente i costruttori.
- Nessuna reflection per la DI, nessun container globale, nessun lookup runtime.
- Una dipendenza mancante, un binding ambiguo o un ciclo fanno fallire la generazione.
- Il codice generato deve essere leggibile, deterministico e verificabile in CI.
- Cartelle, nomi di package, tipi e costruttori non determinano la registrazione
  o il ruolo di un componente.

La generazione è un passo esplicito prima di `go build`: Go non esegue
automaticamente `go generate` durante la compilazione.

## Pipeline

1. **Frontend:** carica i package selezionati dalla composizione dell'utente,
   rispettando moduli, build tag e target; legge registrazioni e firme con
   `go/ast` e `go/types`, senza assumere directory o prefissi speciali.
2. **Grafo:** indicizza i provider per identità di tipo Go. I binding espliciti
   hanno precedenza; `AutoBind()` abilita per modulo la selezione automatica
   di un'implementazione concreta unica. Più candidati richiedono un binding manuale.
3. **Piano:** verifica le registrazioni e ordina i provider raggiungibili dalle
   radici. Una singola istanza per provider e per inizializzazione.
4. **Emitter:** genera il composition root, propaga gli errori dei costruttori
   e gestisce il cleanup inverso delle risorse tramite `BuildWithCleanup`.
5. **CLI:** espone generazione, verifica del codice aggiornato e ispezione del
   grafo in formato testuale/JSON, riutilizzando l'analisi del generatore.

`internal/di` implementa il resolver. `internal/generate` carica i template
con registrazioni esplicite, costruisce il grafo ed emette il wiring.
`cmd/pfw` espone la CLI. L'esempio `examples/httpapi` utilizza il codice generato.
Il framework completo è ancora in sviluppo.

`lifecycle` implementa avvio, supervisione, arresto e cleanup finale dei
componenti. `transport/httpserver` collega un server della standard library a
quel contratto. La [guida lifecycle](lifecycle.md) ne descrive API e limiti.

`http` offre risposte, errori pubblici, serializzazione/deserializzazione JSON e
mapping degli errori applicativi. Questi helper usano `net/http`, sono opzionali
e non richiedono un context o un router Palma. La [guida HTTP](http.md) ne mostra l'uso.

`config` carica e valida l'environment in struct Go prima della composizione
dei servizi. Il valore viene passato come input all'initializer generato.
La [guida configurazione](configuration.md) ne descrive il contratto.

## Libertà di organizzazione del codice

La composizione deve identificare i costruttori tramite simboli Go e i binding
tramite tipi. Una funzione può chiamarsi `NewUserService`, `BuildUsers` o
`Create`: il nome non la rende automaticamente un provider. Allo stesso modo,
un tipo non deve avere un suffisso `Controller`, `Service` o `Repository`.
Il nome descrittivo del provider nel resolver serve alla diagnostica; la
risoluzione delle dipendenze usa l'identità dei tipi Go.

La registrazione esplicita e la discovery annotata sono entrambe disponibili.
`Discover` è opzionale e limitato ai package selezionati, senza vincolare
l'applicazione a cartelle come `controllers/`, `services/` o `repositories/`.
Anche i moduli Palma sono gruppi logici di registrazioni: possono raccogliere
provider da più package e non devono coincidere con una directory o un modulo Go.

- In MVC, i controller possono usare direttamente servizi o model registrati.
- In un'architettura esagonale, i servizi applicativi possono dipendere da
  interfacce definite dall'applicazione, con binding automatici o espliciti agli adapter.
- In un'applicazione piccola, handler e costruttori possono convivere in un package.

Questi stili devono utilizzare lo stesso nucleo DI e le stesse funzionalità
HTTP, senza modalità del framework specifiche per ciascuna architettura.
I servizi applicativi non devono incorporare un tipo base Palma o implementare
un'interfaccia marker per poter essere iniettati.

Il generatore deve permettere di scegliere il package e il file di destinazione
del composition root. Restano validi i normali vincoli Go: visibilità dei
simboli, regole dei package `internal` e assenza di cicli negli import.
Se il codice generato non può accedere a un costruttore, deve produrre una
diagnostica utile; non deve aggirare questi vincoli o spostare il codice utente.
Il primo generatore emette nel package del template, con nome file configurabile.
Per scegliere un altro package si colloca il template in quel package.

Lo scaffold potrà offrire layout di esempio opzionali. Generazione, build e
funzionamento non devono richiedere di adottare uno di quei layout.

## Backend e confine fra trasporti

Il framework distingue tre responsabilità logiche, senza richiedere che
corrispondano a cartelle o livelli separati nell'applicazione:

- **Nucleo:** DI a build time, composizione dei moduli, configurazione e lifecycle.
- **Servizi applicativi:** operazioni del backend, con dipendenze esplicite e
  contratti Go. Quando serve, ricevono `context.Context` per cancellazione e
  deadline; non dipendono dai tipi del trasporto.
- **Adapter di trasporto:** espongono i servizi e traducono richieste, risposte
  ed errori secondo il protocollo. HTTP è il primo adapter.

Per esempio, un servizio `CreateUser(ctx, input)` può essere chiamato sia da
un controller HTTP sia, in futuro, da un handler gRPC. Il controller HTTP
conosce header, status code e JSON; il servizio conosce l'operazione da eseguire.
Non serve obbligare tutte le applicazioni a una struttura a livelli: questo
confine diventa utile quando una stessa operazione deve essere riutilizzata.

### Prima versione HTTP

L'esperienza API deve coprire routing, controller costruiti tramite DI,
decodifica delle richieste, validazione dell'input, risposte JSON, traduzione
degli errori e middleware attraverso integrazioni opzionali con la libreria
HTTP scelta dall'applicazione. La sintassi pubblica di controller e route
resta da decidere.

### Scelta della libreria HTTP

Si distinguono la composizione dei servizi, il lifecycle del server e la
registrazione delle route:

- **DI:** può costruire router, handler e controller come qualsiasi altro
  provider Go, senza richiedere un tipo di router comune.
- **Lifecycle:** un'integrazione collega il server all'avvio, alla gestione
  degli errori e all'arresto dell'applicazione.
- **Routing e handler:** ogni integrazione conserva i tipi, le API e i
  middleware nativi della libreria scelta.

Il nucleo non deve importare Echo, Chi o Gin. Le integrazioni devono essere
selezionabili separatamente, così un'applicazione dipende soltanto dalle
librerie che utilizza. L'integrazione con la standard library è la prima
proposta da implementare; le altre potranno essere ufficiali o sviluppate
dall'utente. La composizione manuale di una libreria deve restare possibile
anche senza un'integrazione dedicata.

Non si introduce un context HTTP universale obbligatorio o un router astratto
che richieda di reimplementare tutte le funzionalità delle librerie. Binding,
serializzazione ed error handling possono avere helper opzionali per ciascuna
integrazione; non devono sostituire forzatamente quelli nativi. Il confine del
lifecycle riguarda la gestione del server, non impone `net/http` a ogni backend.

La sostituibilità riguarda il nucleo Palma e i servizi indipendenti dal
trasporto. Cambiare libreria HTTP può richiedere di adattare route, handler
e middleware che utilizzano le sue API: non si promette una migrazione senza
modifiche a questi componenti.

Autenticazione e autorizzazione devono poter utilizzare servizi iniettati;
gli header e le credenziali si estraggono nel trasporto, mentre le regole
applicative possono essere condivise. OpenAPI è un possibile passo successivo,
senza essere un prerequisito del nucleo DI.

### Lifecycle e trasporti futuri

Il lifecycle dell'applicazione deve gestire le risorse condivise e i server:
inizializzazione delle dipendenze, avvio, segnalazione degli errori di esecuzione,
arresto con deadline e cleanup. Se l'avvio fallisce a metà, le risorse già
inizializzate devono essere rilasciate.

Un adapter futuro registra i propri costruttori e partecipa a questo lifecycle.
HTTP e gRPC potrebbero così convivere nello stesso processo e utilizzare le
stesse istanze dei servizi. Il lifecycle condiviso non deve imporre un unico
modello di richiesta, risposta o middleware ai diversi protocolli.

Per la prima versione si implementa soltanto ciò che serve a HTTP. Il confine
di integrazione con il lifecycle si verifica con un server HTTP e una seconda
libreria HTTP prima di stabilizzare l'API pubblica. Le estensioni specifiche
per altri trasporti si definiscono quando esiste un caso concreto, come gRPC.

## Scelte proposte, ancora da confermare

- Manifest Go esplicito per registrare costruttori, binding e moduli (implementato).
- Scope singleton per inizializzazione come primo default.
- Firme supportate: `func(...) T`, `func(...) (T, error)` e
  `func(...) (T, func() error, error)` per le risorse.
- Configurazione caricata e validata all'avvio, esposta come tipi Go.
- Moduli espliciti per HTTP, persistence e integrazioni; il nucleo DI resta autonomo.
- Lifecycle applicativo con avvio, arresto, segnali e cleanup ordinato.
- Prima integrazione HTTP con la standard library, mantenendo indipendenti il
  nucleo e la scelta della libreria; routing e convenzioni controller si decidono dopo.

La [guida dell'API DI](di-api-proposal.md) descrive la sintassi implementata
e il codice di inizializzazione generato. L'API non è ancora stabilizzata.

Il wiring è statico; valori come DSN e segreti restano configurazione runtime.
Le dipendenze per richiesta devono essere modellate con un composition root
dedicato o una factory esplicita, senza condividere lo stato di una richiesta.

## Sequenza di sviluppo

1. Resolver tipizzato e diagnostica, con test dei casi negativi.
2. Sintassi di registrazione concordata e generatore end-to-end.
3. Errori dei costruttori, risorse e lifecycle.
4. Moduli, configurazione e prima applicazione HTTP completa.
5. Tooling: scaffold, grafo, verifica in CI e override per test.

Non si fissano ora ORM, router esterni, annotation syntax o profili: queste
decisioni non devono bloccare il contratto fondamentale della DI.

## Identità del modulo

Il module path scelto è `github.com/palma99/palma-framework`.
Il nome del package Go resta `pfw`, così come quello della CLI e dei marker.
