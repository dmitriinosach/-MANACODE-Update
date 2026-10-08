# ManacodeUpdate

Программа ставит и обновляет аддоны Manacode для WoW 3.3.5a (WoW Circle) и скачивает данные для PlayerRaidsInfo.

| Аддон | Что делает | Где скачать вручную |
|---|---|---|
| PlayerRaidsInfo | статистика рейдов игроков: окно `/raids` и карточка по Alt. **Работает только на WoW Circle** — статистика собрана из логов его серверов | [GitHub](https://github.com/dmitriinosach/-MANACODE-PlayerRaidsInfo/releases/latest), данные — [ветка data](https://github.com/dmitriinosach/-MANACODE-PlayerRaidsInfo/tree/data) |
| Raid Helper | разбор рейда по записи и сборщик состава | [GitHub](https://github.com/dmitriinosach/-MANACODE-RaidHelper/releases) |
| Raid Waiting Arcade | мини-игры, пока собирается рейд | [GitHub](https://github.com/dmitriinosach/-MANACODE-RaidWaitingArcade/releases/latest) |
| LazyBuffBar | панель бафов: что держать твоему классу и спеку | [GitHub](https://github.com/dmitriinosach/-MANACODE-LazyBuffBar/releases) |
| Vendor | окно торговца: весь товар одним списком, группы, фильтр, покупка пачкой | [GitHub](https://github.com/dmitriinosach/-MANACODE-Vendor/releases) |

Программа берёт всё отсюда же, с GitHub. Появятся страницы на CurseForge — ссылки будут в таблице и в окне программы.

## Где взять

Скачайте `ManacodeUpdate.exe` из [релизов](https://github.com/dmitriinosach/-MANACODE-Update/releases/latest) и положите куда удобно: в `Interface\AddOns` или на рабочий стол. Лежит в игре — найдёт её сама, иначе один раз спросит папку игры и запомнит. Программа одна на все аддоны. Когда выходит её новая версия, внизу окна появляется «Вышла ManacodeUpdate X — скачать вручную»: щелчок открывает страницу релиза, новый exe кладётся на место старого.

## Как пользоваться

Слева — аддоны, справа — выбранный: версия и кнопка «Установить» или «Обновить». У PlayerRaidsInfo там же сезоны и кнопка «Скачать»; после скачивания наберите в игре `/reload`. Перед установкой закройте игру полностью.

Переключатель внизу включает автообновление: программа работает в фоне без окна, данные проверяет каждые 15 минут, аддоны и саму себя — раз в 4 часа. Включается задачей Планировщика Windows при входе, прав администратора не нужно.

Вопросы — в [Discord](https://discord.gg/hpTkJCJbwn).

## Сборка из исходников

Нужен Go (версия — в `go.mod`) на Windows:

```
go test ./...
go build -trimpath "-ldflags=-H=windowsgui -X main.zipPass=raids-circle" -o ManacodeUpdate.exe .
```

Релизы собирает GitHub Actions из этих исходников (`.github/workflows/release.yml`) по тегу `vX.Y.Z`.

## Privacy policy

This program will not transfer any information to other networked systems unless specifically requested by the user or the person installing or operating it.

What it does over the network, and only that:

- downloads addon releases and PlayerRaidsInfo data and checks for a new version of itself on GitHub (`api.github.com`, `github.com` and GitHub download hosts such as `raw.githubusercontent.com`) — when you open the program, press a button, or turn on auto-update;
- sends no personal data: the requests carry only the program name and version (`User-Agent: ManacodeUpdate/X.Y.Z`).

On your computer it writes only to the game folder (`Interface\AddOns`, its settings and log in `Interface\AddOns\Manacode`), to `%APPDATA%\ManaCode\ManacodeUpdate.json` (where the game is) and, if you turn auto-update on, creates a Windows Task Scheduler task `Manacode_Update` for your user. When you install a Raid Helper test record, it writes that record into `WTFAccount<account>SavedVariables` of the account you pick and keeps a copy of your own. On start it stops and removes previous Manacode updaters (`ОбновитьДанные.exe`, `ОбновитьАддоны.exe`, `RaidHelperUpdate.exe`) and their Task Scheduler tasks.

## Лицензия

[MIT](LICENSE).
