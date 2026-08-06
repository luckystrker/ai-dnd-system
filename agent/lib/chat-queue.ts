/**
 * Последовательная обработка сообщений одного чата.
 *
 * Вебхук Telegram вызывает send() в waitUntil для каждого апдейта. Если два
 * сообщения приходят практически одновременно (обычный случай — юзер быстро
 * пишет два сообщения подряд), eve стартует два workflow-рана одной сессии
 * параллельно, и второй ран падает с HookConflictError: delivery-хук сессии
 * занят первым раном (`Hook token "telegram:<chatId>:" is already in use`).
 * Сообщение при этом теряется, юзеру уходит «Сессия не восстановилась...».
 *
 * Очередь на ключ чата исключает гонку: следующий апдейт обрабатывается
 * только после того, как завершился предыдущий send() того же чата.
 */
export class PerChatQueue {
  private readonly tails = new Map<string, Promise<void>>();

  get size(): number {
    return this.tails.size;
  }

  /**
   * Запускает task после всех ранее поставленных задач того же chatKey.
   * Разные ключи выполняются параллельно. Падение задачи не блокирует
   * следующие, но пробрасывается в возвращаемый промис.
   */
  enqueue<T>(chatKey: string, task: () => Promise<T>): Promise<T> {
    const prev = this.tails.get(chatKey) ?? Promise.resolve();
    const run = prev.then(task, task);
    const tail = run.then(() => undefined, () => undefined);
    this.tails.set(chatKey, tail);
    void tail.then(() => {
      // Убираем ключ из карты, только если с тех пор не пришла новая задача.
      if (this.tails.get(chatKey) === tail) this.tails.delete(chatKey);
    });
    return run;
  }
}

/** Очередь вебхука Telegram-канала. */
export const telegramChatQueue = new PerChatQueue();
