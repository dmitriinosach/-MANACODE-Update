# ManacodeUpdate

Программа ставит и обновляет аддоны Manacode для WoW 3.3.5a (WoW Circle), скачивает данные для PlayerRaidsInfo и поддерживает свои проекты аддонов с публичными релизами GitHub.

| Аддон | Что делает | Где скачать вручную |
|---|---|---|
| PlayerRaidsInfo | статистика рейдов игроков: окно `/raids` и карточка по Alt. **Работает только на WoW Circle** — статистика собрана из логов его серверов | [GitHub](https://github.com/dmitriinosach/-MANACODE-PlayerRaidsInfo/releases/latest), данные — [ветка data](https://github.com/dmitriinosach/-MANACODE-PlayerRaidsInfo/tree/data) |
| Raid Helper | разбор рейда по записи и сборщик состава | [GitHub](https://github.com/dmitriinosach/-MANACODE-RaidHelper/releases) |
| Raid Waiting Arcade | мини-игры, пока собирается рейд | [GitHub](https://github.com/dmitriinosach/-MANACODE-RaidWaitingArcade/releases/latest) |
| LazyBuffBar | панель бафов: что держать твоему классу и спеку | [GitHub](https://github.com/dmitriinosach/-MANACODE-LazyBuffBar/releases) |
| Vendor | окно торговца: весь товар одним списком, группы, фильтр, покупка пачкой | [GitHub](https://github.com/dmitriinosach/-MANACODE-Vendor/releases) |
| Market Based Auction | аукцион: крафты, реагенты, распыление и общая история рынка | [GitHub](https://github.com/dmitriinosach/-MANACODE-MarketBasedAuction/releases) |
| Talents | билдер талантов, осмотр, сравнение и обмен; бета | [GitHub](https://github.com/dmitriinosach/-MANACODE-Talents/releases) |

Программа берёт всё отсюда же, с GitHub. Появятся страницы на CurseForge — ссылки будут в таблице и в окне программы.

## Где взять

Скачайте `ManacodeUpdate.exe` из [релизов](https://github.com/dmitriinosach/-MANACODE-Update/releases/latest) и положите куда удобно: в `Interface\AddOns` или на рабочий стол. Лежит в игре — найдёт её сама, иначе один раз спросит папку игры и запомнит. Программа одна на все аддоны. Когда выходит её новая версия, внизу окна появляется «Вышла ManacodeUpdate X — обновить»: щелчок скачивает её с GitHub, заменяет exe и перезапускает окно. С включённым автообновлением программа обновляется сама в фоне. Если запустить новую копию из другой папки, автообновление переходит на неё, а старая фоновая копия останавливается.

## Как пользоваться

Слева — аддоны, справа — выбранный: версия и кнопка «Установить» или «Обновить». У PlayerRaidsInfo там же сезоны и кнопка «Скачать»; после скачивания наберите в игре `/reload`. Перед установкой закройте игру полностью.

Переключатель внизу включает автообновление: программа работает в фоне без окна, данные проверяет каждые 15 минут, аддоны и саму себя — по выбранному интервалу: 1, 2, 4, 6, 12 или 24 часа. По умолчанию — 4 часа; интервал меняется кнопкой «Аддоны» внизу окна. Включается задачей Планировщика Windows при входе, прав администратора не нужно.

Интерфейс доступен на русском, английском и испанском. Первый запуск предлагает выбрать язык; ссылка «RU / EN / ES» под логотипом открывает выбор повторно.

В карточке установленного аддона есть «Удалить аддон»: перед удалением программа показывает папки и запрашивает подтверждение. Настройки персонажей в `WTF` сохраняются. Для повторной установки нажмите «Установить»; при работе с папками аддонов игра должна быть закрыта.

### Свои аддоны с GitHub

Над списком аддонов нажмите «+ Свой GitHub-проект». Вставьте ссылку на публичный проект — например, [DefileAlert](https://github.com/suprepupre/DefileAlert). Подойдёт также ссылка на релиз, ветку или файл: программа сама найдёт последний стабильный релиз проекта.

Нажмите «Найти релиз», выберите архив для вашей версии WoW и нажмите «Добавить проект». Аддон появится слева; в его карточке нажмите «Установить». Поддерживаются ZIP, 7z, RAR и TAR, в том числе `.tar.gz`, `.tar.bz2` и `.tar.xz`. Внешний архиватор не нужен. Если готового архива нет, можно выбрать ZIP исходников опубликованного релиза; внутри должен быть аддон WoW с `.toc`. Совместимость с клиентом уточняйте у автора.

Все папки из архива проверяются и устанавливаются только в выбранную `Interface\AddOns`. При первой установке уже существующую папку нужно перенести вручную; встроенные аддоны, папки разработки и junction не заменяются. Архивы с паролем и несколькими томами не поддерживаются.

Если автор изменит набор папок в новом релизе, автоматическая установка остановится до замены файлов. Удалите установленный аддон кнопкой в его карточке, выберите подходящий архив и установите заново; настройки в `WTF` останутся.

Установленные проекты обновляются вместе с обычными аддонами, если включено автообновление. «Сменить архив» меняет выбранный вариант, «Убрать из списка» отключает сопровождение проекта и оставляет файлы на месте. Инструкция «Что нового» показывается при первом запуске и доступна по ссылке под логотипом.

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
- GitHub update requests do not include your raid records or game account credentials; they identify the app using `User-Agent: ManacodeUpdate/X.Y.Z`;
- if you connect the optional Market Based Auction exchange, authenticates with the configured market service, uploads auction scans from the selected account, and downloads shared market history. These credentials belong to the market service, not your WoW account. The password is not saved; the saved access token is protected with Windows DPAPI;
- if you configure a Discord webhook, sends Raid Helper summary images to that webhook. Without a webhook, summaries stay on your computer.

On your computer it installs and removes addon files inside the selected game's `Interface\AddOns`, and stores settings, logs and exchange caches in `%APPDATA%\ManaCode`. Addon removal preserves saved settings in `WTF`. If you turn auto-update on, it creates a Windows Task Scheduler task `Manacode_Update` for your user. Installing a Raid Helper test record writes to the selected account's `WTF\Account\<account>\SavedVariables` and keeps a backup. The Lich King map fix writes a patch inside the game's `Data` folder; the optional 4 GB memory button modifies `Wow.exe` after saving a backup. Self-updates replace the updater executable. On start it stops and removes previous Manacode updaters (`ОбновитьДанные.exe`, `ОбновитьАддоны.exe`, `RaidHelperUpdate.exe`) and their Task Scheduler tasks.

## Лицензия

[MIT](LICENSE).
