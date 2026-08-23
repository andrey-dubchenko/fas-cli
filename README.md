# FAS CLI: Flink Admin Schema Manager

**v1.0.0** | [Andrey Dubchenko](https://github.com/andrey-dubchenko) | [Stanislav Agarkov](https://github.com/agarkoff)

An interactive terminal application for managing Apache Flink schemas, projections, and ReadViews across multiple environments.

## Key features

- **Schema browser**: list every job/projection, with optional module filtering
- **OpenSearch indices**: automatic lookup and display of the index bound to each schema
- **Document counts**: shows how many documents are in each bound index
- **Schema operations**:
  - Stop: deactivate a job/projection
  - Start: activate a job/projection
  - **Restart**: stop, then start
  - Delete: remove a schema and its index
  - Move Alias: move an alias between two versions of the same schema
- **Multiple environments**: switch between named environments, with the option to disable any of them
- **Interactive TUI**: built on [tview](https://github.com/rivo/tview)/[tcell](https://github.com/gdamore/tcell)

## Requirements

- Go 1.25.4+
- Access to a Flink Admin API (schema management endpoints)
- Access to OpenSearch (optional, see "Configuration" below)

## Installation

### From source

```bash
git clone <repository>
cd fas-cli
go build -o fas-cli ./cmd/fas-cli
```

### Usage

```bash
# Show version and authors
./fas-cli -version

# Run the app
./fas-cli [-module <module_name>] [-config config.json] [-env environment]
```

**Flags:**
- `-module`: module name (optional; falls back to the config's default)
- `-config`: path to config.json (optional; discovered automatically)
- `-env`: initial environment (optional; falls back to the config's default)

**Finding and generating config.json:**
1. If `-config` is given, that path is used.
2. Otherwise the app looks in this order:
   - `config.json` in the current directory
   - `config.json` next to the executable
   - `config.json` in the working directory
3. **If no file is found, the app creates an empty one** (no modules, no environments; add them yourself, see below).

**Examples:**
```bash
# Use everything from the config
./fas-cli

# Override the module
./fas-cli -module moduleA

# Override the environment
./fas-cli -env PROD

# Point at a specific config file
./fas-cli -config /etc/fas-cli/config.json
```

## Configuration

### config.json

```json
{
  "modules": ["moduleA", "moduleB"],
  "defaultModule": "moduleA",
  "defaultEnv": "TEST",
  "endpoints": {
    "schemaList": "/v2/schema/short",
    "schemaStop": "/v2/schema/{name}/{version}/stop",
    "schemaStart": "/v2/schema/{name}/{version}/start",
    "schemaDelete": "/v2/schema/{name}/{version}"
  },
  "environments": [
    {
      "name": "DEV",
      "flinkAdmin": "https://flink-admin.example.com",
      "opensearch": "https://opensearch.example.com",
      "disabled": false
    },
    {
      "name": "TEST",
      "flinkAdmin": "https://flink-admin.test.example.com",
      "opensearch": "https://opensearch.test.example.com",
      "disabled": false
    },
    {
      "name": "PROD",
      "flinkAdmin": "https://flink-admin.prod.example.com",
      "opensearch": "https://opensearch.prod.example.com",
      "disabled": true
    }
  ]
}
```

**Top-level fields:**
- `modules`: list of available modules. Optional: if it's missing or empty, no module filter is applied (jobs/schemas from every module are shown), and the `1-9` module switch is disabled.
- `defaultModule`: module used on startup.
- `defaultEnv`: environment used on startup.
- `environments`: can also be empty. The minimal valid config looks like this:
  ```json
  {
    "modules": [],
    "defaultModule": "DEFAULT",
    "defaultEnv": "DEFAULT",
    "environments": [],
    "endpoints": {}
  }
  ```
  The app starts fine with this and just shows "No environments configured" instead of a schema list. Add an environment to the file by hand, then in the app press `c` then **Reload from disk**. The config editor form can only edit existing environments (toggle `disabled`, change the defaults); it cannot create new ones yet.

**Environment fields:**
- `name`: environment name.
- `flinkAdmin`: base URL of the Flink Admin service.
- `opensearch`: base URL of OpenSearch (optional, see below).
- `disabled`: disable this environment (true/false).

**`endpoints`: Flink Admin API path templates**

Path templates the app calls for Stop/Start/Restart/Delete and the schema list; `{name}`/`{version}` are substituted automatically. The defaults match the current (non-deprecated) tag in Flink Admin's own OpenAPI document. If a particular instance only exposes an older or differently versioned API contract, point these at whatever that instance actually serves, no code changes needed:

| Field | Default | Method |
|---|---|---|
| `schemaList` | `/v2/schema/short` | GET (paginated via `page_num`/`page_size`) |
| `schemaStop` | `/v2/schema/{name}/{version}/stop` | POST |
| `schemaStart` | `/v2/schema/{name}/{version}/start` | POST, body `{"startArg":[]}` |
| `schemaDelete` | `/v2/schema/{name}/{version}` | DELETE, `?deleteIndex=false` is appended |

The `endpoints` section can be omitted entirely; the defaults above are then used (existing config.json files from before this option existed keep working unchanged).

**When an environment has no `opensearch`:**

The `opensearch` field can be left as an empty string. In that case:
- the index, alias, and document count columns always show "no data", with no errors during loading;
- the `m` (Move Alias) key disappears from the help bar and does nothing, since there is nothing to move an alias between.

## Controls

### Navigation keys

| Key | Description |
|---------|----------|
| `↑` / `k` | Up |
| `↓` / `j` | Down |
| `Enter` | Expand/collapse a group |
| `Space` | Toggle selection |
| `a` | Select/deselect all |
| `f` | Search by schema name |
| `c` | Open the config editor |
| `?` / `h` | Full keyboard shortcut reference |

### Operation keys

| Key | Description | Example |
|---------|----------|---------|
| `F1-F9` | Switch environment | `F1` is the first environment, `F5` the fifth |
| `1-9` | Switch module | `2` is the second module in the list |
| `s` | **Stop** the selected items | `s STOP (2)` |
| `r` | **Restart** the selected items | `r RESTART (2)` |
| `d` | **Delete** the selected items | `d DELETE (2)` |
| `m` | **Move Alias** (2 versions of the same schema; needs `opensearch` in the config) | `m MOVE alias` |
| `c` | **Config**: open the config editor | |
| `?` / `h` | Show the full keyboard shortcut reference | |
| `q` / `Ctrl+C` | Quit | |

Letter shortcuts work regardless of keyboard layout: both a US layout and a Cyrillic (ЙЦУКЕН) layout work.

`Esc` **does not quit the app**. It only closes an open dialog (search/config/help), or does nothing on the main screen. Quitting is only `q` or `Ctrl+C`.

### Searching by schema name

Press **`f`**: a small search box opens on top of the list.

```
+ Search by schema name ------------------+
| Search: example                          |
+-------------------------------------------+
```

- Type text to filter schemas by name; the list behind the dialog filters live.
- `Enter` closes the box, keeping the filter applied.
- `Esc` closes the box and clears the filter (all schemas show again).
- `Backspace` deletes the last character.

### Switching modules

Switch at any time with `1`...`9` (`1` is the first module in config.json, `2` the second, and so on). Environments switch separately, with `F1`...`F9`.

- Press a digit to select a module.
- Schemas reload automatically.

### Adapting to window size

The app adapts to the terminal size automatically:

- **Fixed header**: the environment tabs and title are always visible.
- **Scrolling list**: the schema list scrolls like a normal widget (arrows, PgUp/PgDn, mouse wheel), with no manual pagination.
- **Dynamic layout**: the list takes up all available space; the results panel resizes to fit the number of result lines.

**Recommended window size:** 120x30
**Minimum window size:** 80x24

### Typical workflow

1. **Pick an environment**: press `F1-F9` to switch.
2. **Pick a module**: start with `-module moduleA`, or press `1-9`.
3. **Search for a schema**: press `f` and type a name to filter.
4. **Expand a group**: `Enter` on a schema name (after loading, and after every environment/module switch, all groups start collapsed).
5. **Select versions**: `Space` on the versions you want.
6. **Run an operation**: press `s`, `r`, `d`, or `m`.
7. **Check the results**: operations run one after another, with results shown as they complete.

## Example operations

### Stop a job

```
Select:
  [x] example_schema/1.0.0
  [x] example_schema/1.0.1

Press: s

Result:
  OK   example_schema/1.0.0 stopped
  OK   example_schema/1.0.1 stopped
```

### Restart a job (stop, then start)

```
Select:
  [x] example_schema/1.0.0

Press: r

Result:
  OK   example_schema/1.0.0 stopped
  OK   example_schema/1.0.0 started
```

### Move an alias between versions

```
Select (exactly 2 versions of the same schema):
  [x] example_schema/1.0.0 (has the alias)
  [x] example_schema/1.0.1 (the new version)

Press: m

Result:
  OK   Moved aliases from example_schema-1.0.0 to example_schema-1.0.1
```

### Delete a schema

```
Select:
  [x] example_schema/0.9.0

Press: d

Result:
  OK   example_schema/0.9.0 schema deleted
  OK   example_schema/0.9.0 index deleted (example_schema-0.9.0)
```

## Editing the config from within the app

Press **`c`**: a config editor form opens on top of the list.

```
+ Config: /path/to/config.json --------------------+
| Default module        [ moduleA           v]     |
| Default environment   [ TEST              v]     |
| Disable DEV            [ ]                       |
| Disable TEST           [ ]                       |
| Disable PROD           [x]                       |
|                                                   |
|  < Save >  < Reload from disk >  < Cancel >      |
+---------------------------------------------------+
```

- `Tab` / arrows move between fields and buttons; `Enter`/`Space` selects.
- **Default module** / **Default environment**: dropdowns that set which module/environment is used the next time the app starts.
- **Disable `<env>`**: checkbox for that environment's `disabled` flag in `config.json`.
- **Save**: writes the changes to `config.json` and applies them immediately.
- **Reload from disk**: discards the form's edits and rereads `config.json` from disk.
- **Cancel** / `Esc`: close the form without saving.

## Disabling environments

To temporarily disable an environment (during maintenance, for example), set `"disabled": true`:

```json
{
  "name": "PROD",
  "disabled": true
}
```

**Result in the interface:**
```
F1:DEV  F2:TEST  F3:UAT  F4:INT  F5:PROD [disabled]
```

Trying to switch to a disabled environment shows:
```
environment PROD is disabled
```

## Interface

### Schema table

After loading (and after every environment/module switch) all schema groups start collapsed; expand the ones you need with `Enter`:

```
Flink Schemas: module moduleA

F1:DEV (active)  F2:TEST  F3:UAT  F4:INT  F5:PROD

> example_schema (3 versions)
> another_schema
```

After `Enter` on `example_schema`:

```
v example_schema (3 versions) [1 selected]
    (X) 1.0.0   ACTIVE    example_schema-1.0.0  alias  100k docs
    ( ) 1.0.1   INACTIVE  example_schema-1.0.1  -      0 docs
    ( ) 1.1.0   PAUSED    example_schema-1.1.0  alias  500k docs
> another_schema

Results:
  OK   example_schema/1.0.0 stopped
  OK   example_schema/1.0.0 started

Up/down nav | Enter/Space | F1-F9 env | 1-9 module | f search | c config | ? help | q quit | s/r/d (1)
```

The bottom help line is kept short on purpose so it never wraps or gets clipped; the full shortcut list is behind `?`.

### Color key

- **Blue**: header, active tab
- **Green**: a selected item
- **Yellow**: status (ACTIVE, INACTIVE, PAUSE)
- **Magenta**: aliases and index names
- **Gray**: document counts, missing data
- **Red**: errors, the PROD environment, disabled environments

## API endpoints used

The app calls the following Flink Admin / OpenSearch endpoints (paths are the defaults; see the `endpoints` config section to override them):

```
GET    <schemaList>                 list schemas (paginated)
POST   <schemaStop>                 stop
POST   <schemaStart>                start
DELETE <schemaDelete>               delete
GET    /_alias                      list aliases (OpenSearch)
GET    /_cat/indices                index stats (OpenSearch)
POST   /_aliases                    move an alias (OpenSearch)
```

## Troubleshooting

### Network errors

If a request fails, check:
1. That the Flink Admin URL is reachable.
2. That OpenSearch is reachable (if configured).
3. That the module name is correct.

### Example error

```
flink HTTP 404: Schema not found
```

Fix: check that the module and schema name are correct.

## Code layout

The interface is built on [tview](https://github.com/rivo/tview)/[tcell](https://github.com/gdamore/tcell). Sources live in `cmd/fas-cli/`:

```
cmd/fas-cli/
|-- main.go      entry point: flags, finding and loading config.json
|-- config.go    Config, Environment, reading/writing config.json
|-- flink.go     Flink/OpenSearch response types and all backend calls
|                (fetchSchemas, processNextOp, moveAlias, ...)
|-- app.go       the App struct, tview widget setup (NewApp/Run)
|-- render.go    UI redraws: header, table, status panel, help bar
|-- actions.go   action handlers: stop/restart/delete/move,
|                environment/module switching
|-- keys.go      layout-independent keyboard shortcuts (handleKey)
`-- modals.go    modal dialogs: config, search, help
```

## Supported operations

### Stop

Deactivates a job/projection. Data stops updating.

```
API: POST <schemaStop>
Response: 204 No Content
```

### Start

Activates a job/projection so data starts updating again.

```
API: POST <schemaStart>
Body: {"startArg":[]}
Response: 204 No Content
```

### Restart

Runs Stop, then Start, in sequence. Used to reinitialize a job.

```
Operation: stop, then start
```

### Delete

Removes a schema from Flink Admin and, optionally, its index from OpenSearch.

```
API: DELETE <schemaDelete>?deleteIndex=false
Response: 200 OK
```

### Move Alias

Moves an alias from one index to another. Used when migrating to a new version.

```
API: POST /_aliases
Body: {
  "actions": [
    {"remove": {"index": "old", "alias": "name"}},
    {"add": {"index": "new", "alias": "name"}}
  ]
}
```

## Usage examples

### Upgrading to a new version

```bash
# 1. Run the app (uses the config's defaults)
./fas-cli

# Or with overrides:
./fas-cli -module moduleA -env DEV

# 2. In the interface:
# - Expand example_schema (Enter)
# - Select the old version (Space)
# - Select the new version (Space)
# - Press 'm' to move the alias
```

### Restarting a job

```bash
# 1. Run the app (module and environment from the config)
./fas-cli

# 2. In the interface:
# - Select example_schema/1.0.0 (Space)
# - Press 'r' to restart (stop, then start)
```

### Removing old versions

```bash
# 1. Run the app
./fas-cli

# 2. In the interface:
# - Select example_schema/0.9.0 (Space)
# - Press 'd' (delete)
# - Confirm the operation completed in the results panel
```

## Authors

[Andrey Dubchenko](https://github.com/andrey-dubchenko) | [Stanislav Agarkov](https://github.com/agarkoff)
