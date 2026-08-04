import { telegramChannel } from "eve/channels/telegram";

export default telegramChannel({
  botUsername: process.env.TELEGRAM_BOT_USERNAME,
});
