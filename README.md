# WholesaleAll
Automatically trades all configured coins to destination coins

#### Example config for deposits:

    "sell_all": {
      "eth": {
        "usdt": 100
      },
      "rvn": {
        "etc": 50
      }
    }

* Trade 100% of deposited ETH to USDT
* Trace 50% of RVN via USDT to ETC

### Telegram notifications:

    "bot_token": Your created bot token
    "bot_chat_id": Your telegram id (bot will reply with your id if this empty/zero)

* Will ignore other users if configured with bot_chat_id 