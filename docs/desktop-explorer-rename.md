# Verified Explorer rename

The desktop rename recipe removes model round trips between selecting a file,
opening its inline editor, replacing its name and checking the committed result.
It operates through the Windows UI, with the existing child permissions and hooks.

```json
{"desktop":{"mode":"rename","window_id":"OBSERVED_ID","rename":{"old_name":"hesap","new_name":"sonuc"}}}
```

The window must be a verified foreground Explorer folder window. Names follow
Explorer's current extension display policy: use `hesap.txt` and `sonuc.txt` when
extensions are visible. The recipe refuses unsafe leaf names and existing targets.

SelectionItem selects exactly one fresh ListItem without opening it. The provider
reads its selection container and confirms keyboard focus before F2. A fresh
focused descendant must be an Edit or legacy Pane inside that item's bounds,
with the exact original name. Replacement text is read back in the same editor
before Enter. Editor closure, disappearance of the original name and appearance
of the replacement are checked before reporting success.

Failure stops dependent inputs, including permission denial or uncertain editor
state. No mutation automatically retries. Unsupported providers require a fresh
observation and another supported strategy. Successful responses are semantic;
no screenshot or filesystem mutation is needed. The call has a 45-second bound.

Regression coverage includes single-selection native provider readback, selection
and focus failures, multiple selected items, incorrect identity, failed child
permissions, foreign editors, incorrect replacement text, unclosed editors,
conflicts, no-op names, invalid names and public pipeline schema validation.
The GUI benchmark's total latency still requires a real run with the selected
provider; these tests do not establish a speed improvement.

The Windows provider reads selected items using
`SelectionPattern.Current.GetSelection()`, as specified in the
[Microsoft UI Automation API](https://learn.microsoft.com/en-us/dotnet/api/system.windows.automation.selectionpattern.selectionpatterninformation.getselection).
The Windows test checks the installed assembly's method/property contract,
and its selection fixture exposes that same API shape. This prevents a fixture
from silently accepting an invented method on SelectionPattern itself.
