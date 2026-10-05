# ManacodeUpdate

Программа ставит и обновляет аддоны Manacode для WoW 3.3.5a (WoW Circle) и скачивает данные для PlayerRaidsInfo.

| Аддон | Что делает | Где скачать вручную |
|---|---|---|
| PlayerRaidsInfo | статистика рейдов игроков: окно `/raids` и карточка по Alt. **Работает только на WoW Circle** — статистика собрана из логов его серверов | [GitHub](https://github.com/dmitriinosach/-MANACODE-PlayerRaidsInfo/releases/latest), данные — [ветка data](https://github.com/dmitriinosach/-MANACODE-PlayerRaidsInfo/tree/data) |
| Raid Helper | разбор рейда по записи и сборщик состава | [GitHub](https://github.com/dmitriinosach/-MANACODE-RaidHelper/releases) |
| Raid Waiting Arcade | мини-игры, пока собирается рейд | [GitHub](https://github.com/dmitriinosach/-MANACODE-RaidWaitingArcade/releases/latest) |
| LazyBuffBar | панель бафов: что держать твоему классу и спеку | [GitHub](https://github.com/dmitriinosach/-MANACODE-LazyBuffBar/releases) |

Программа берёт всё отсюда же, с GitHub. Появятся страницы на CurseForge — ссылки будут в таблице и в окне программы.

## Где взять

Скачайте `ManacodeUpdate.exe` из [релизов](https://github.com/dmitriinosach/-MANACODE-Update/releases/latest) и положите куда удобно: в `Interface\AddOns` или на рабочий стол. Лежит в игре — найдёт её сама, иначе один раз спросит папку игры и запомнит. Программа одна на все аддоны и обновляет себя сама.

## Как пользоваться

Слева — аддоны, справа — выбранный: версия и кнопка «Установить» или «Обновить». У PlayerRaidsInfo там же сезоны и кнопка «Скачать»; после скачивания наберите в игре `/reload`. Перед установкой закройте игру полностью.

Переключатель внизу включает автообновление: программа работает в фоне без окна, данные проверяет каждые 15 минут, аддоны и саму себя — раз в 4 часа. Включается задачей Планировщика Windows при входе, прав администратора не нужно.

Вопросы — в [Discord](https://discord.gg/hpTkJCJbwn).
