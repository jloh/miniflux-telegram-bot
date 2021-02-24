# Miniflux Telegram Bot

**name here** is a Telegram Bot that sends you new Miniflux entries via Telegram.

![](.github/miniflux_bot.png)

## Setup

1. Download the [latest release](https://github.com/jloh/miniflux-telegram-bot/releases/latest/)
1. Place the binary somewhere in your path
1. Set the following environment variables:
  * `MINIFLUX_URL`: Set to the base URL of your Miniflux instance (Defaults to `https://reader.miniflux.app`)
  * `MINIFLUX_API_KEY`: Set to the API key generated for this service
  * `TELEGRAM_BOT_TOKEN`: The Telegram token for your bot (Recieved via the Bot Father)
  * `TELEGRAM_CHAT_ID`: Your Chat ID for yourself and the Bot
1. Start the service
   **Note:** There is a example Systemd `.service` file in the [contrib folder](contrib/)
