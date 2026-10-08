# Primo rilascio: v0.1.0

La prima release è preparata come `v0.1.0`, con licenza MIT e requisito Go 1.26+.
L'API è in evoluzione: la serie `0.x` può introdurre cambiamenti incompatibili.
Il tag identifica la versione del modulo e della CLI; non va spostato dopo la
pubblicazione. Questi passi preparano gli artefatti e una bozza da revisionare.

## Verifica locale

Il wiring incluso nel repository è quello `local`, usato dai test dell'esempio.
Se hai avviato l'esempio con un altro environment, ripristinarlo prima dei check:

```sh
go run ./cmd/pfw generate -env local ./examples/httpapi/internal/bootstrap
bash scripts/verify.sh
bash scripts/package-release.sh v0.1.0
```

La verifica controlla formatting, `go mod tidy`, integrità delle dipendenze,
wiring aggiornato, `go vet`, test con race detector e compilazione. La CI esegue
gli stessi check su Linux, macOS e Windows. Il workflow è configurato ma deve
ancora essere eseguito su GitHub.

Gli artefatti locali sono in `dist/v0.1.0`: CLI per macOS, Linux e Windows, sia
amd64 sia arm64, archivi con LICENSE, licenze delle dipendenze collegate, README
e CHANGELOG, e `checksums.txt` SHA-256.
Ogni binario è compilato senza cgo, con path rimossi e versione incorporata.
Il packaging rifiuta una directory di artefatti già esistente.

```sh
tar -xzf dist/v0.1.0/pfw_v0.1.0_darwin_arm64.tar.gz -C /tmp
/tmp/pfw version
```

Scegliere l'archivio adatto al proprio sistema. `pfw new` usa automaticamente
la versione del binario di release come dipendenza del progetto creato.

## Commit, remote e tag

Prima del tag, rivedere tutte le modifiche del rilascio e includerle nel commit.
Aggiornare la voce del changelog con la data effettiva, rimuovendo "in preparazione".
Non includere credenziali, file dotenv personali o artefatti `dist/`.

Nel checkout attuale non è configurato alcun remote. Dopo aver creato/verificato
il repository GitHub corrispondente al module path, configurarlo:

```sh
git remote add origin git@github.com:palma99/palma-framework.git
```

Questo URL deriva dal module path: verificare che il repository esista e sia
quello desiderato prima di eseguire il comando. Per distribuire il modulo
pubblicamente deve essere accessibile agli utenti.

Committare i file revisionati, pubblicare il branch e attendere la CI verde.
Solo dopo, dal commit approvato:

```sh
git tag -a v0.1.0 -m "Palma Framework v0.1.0"
git push origin v0.1.0
```

Il push del tag attiva `.github/workflows/release.yml`: ripete le verifiche,
crea gli archivi e una **draft release**, usando il tag esistente. Rivedere note
e allegati prima di pubblicarla dalla UI di GitHub. La bozza non nasconde il tag:
il tag remoto rende già risolvibile la versione del modulo Go.

La pipeline usa le action ufficiali [checkout](https://github.com/actions/checkout)
e [setup-go](https://github.com/actions/setup-go), e `gh release create` con
[`--verify-tag` e `--draft`](https://cli.github.com/manual/gh_release_create).
Il workflow ha permessi di scrittura dei contenuti soltanto nel job di release.

## Verifica da consumatore dopo la pubblicazione

In una directory nuova, fuori dal checkout e senza `replace` locale:

```sh
go install github.com/palma99/palma-framework/cmd/pfw@v0.1.0
pfw version
pfw new -template api -router echo -module example.com/checkrelease ./checkrelease
cd checkrelease
go mod tidy
go generate ./internal/bootstrap
go test ./...
go tool pfw run -env dev ./cmd/api
```

Per usare soltanto la libreria in un progetto esistente:

```sh
go get github.com/palma99/palma-framework@v0.1.0
```

L'installazione della versione pubblicata non è verificabile finché commit e tag
non sono disponibili sul remote. `go install` richiede Go anche se la CLI viene
distribuita come binario: i comandi generate/run lavorano su progetti Go.
