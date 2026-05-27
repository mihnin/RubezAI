import { describe, it, expect } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import {
  MessageBubble,
  extractFileAttachments,
  type Message,
} from "../components/MessageBubble";

describe("extractFileAttachments — парсер data:-ссылок от bridge", () => {
  it("без data:-ссылок возвращает оригинальный текст и пустой массив", () => {
    const r = extractFileAttachments("обычный ответ без файлов");
    expect(r.stripped).toBe("обычный ответ без файлов");
    expect(r.files).toEqual([]);
  });

  it("извлекает один файл и убирает его из текста", () => {
    const content =
      "Готово.\n\n📎 Файлы:\n- [📎 report.xlsx](data:application/vnd.openxmlformats-officedocument.spreadsheetml.sheet;base64,YWJj)";
    const r = extractFileAttachments(content);
    expect(r.files).toHaveLength(1);
    expect(r.files[0].name).toBe("report.xlsx");
    expect(r.files[0].mime).toBe(
      "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
    );
    expect(r.files[0].dataUrl).toContain("base64,YWJj");
    expect(r.stripped).toBe("Готово.");
    expect(r.stripped).not.toContain("base64");
    expect(r.stripped).not.toContain("📎 Файлы");
  });

  it("несколько файлов — все вытаскиваются", () => {
    const content =
      "ok\n📎 Файлы:\n- [📎 a.txt](data:text/plain;base64,YQ==)\n- [📎 b.png](data:image/png;base64,Yg==)";
    const r = extractFileAttachments(content);
    expect(r.files).toHaveLength(2);
    expect(r.files.map((f) => f.name)).toEqual(["a.txt", "b.png"]);
  });

  it("data: без 📎 префикса игнорируется (защита от случайных совпадений)", () => {
    // ReactMarkdown сам не пустит data:-ссылку как кликабельную, но мы
    // дополнительно сужаем шаблон до явного маркера 📎.
    const content = "обычный [ссылка](data:text/plain;base64,YQ==)";
    const r = extractFileAttachments(content);
    expect(r.files).toEqual([]);
    expect(r.stripped).toBe(content);
  });
});

describe("MessageBubble — рендер download-кнопок", () => {
  function makeMessage(content: string): Message {
    return { role: "assistant", content };
  }

  it("рендерит кнопку скачивания для файла от модели", () => {
    const msg = makeMessage(
      "Готово.\n📎 Файлы:\n- [📎 cats.xlsx](data:application/octet-stream;base64,YWJj)",
    );
    render(<MessageBubble message={msg} streaming={false} />);
    const block = screen.getByTestId("message-attachments");
    expect(block).toBeInTheDocument();
    const link = screen.getByRole("link", { name: /cats\.xlsx/ });
    expect(link).toHaveAttribute("download", "cats.xlsx");
    expect(link.getAttribute("href")).toContain("base64,YWJj");
  });

  it("если файлов нет — блок не рендерится", () => {
    render(
      <MessageBubble message={makeMessage("просто текст")} streaming={false} />,
    );
    expect(screen.queryByTestId("message-attachments")).toBeNull();
  });

  it("data:-ссылка не остаётся в видимом тексте (UI не показывает base64)", () => {
    const msg = makeMessage(
      "результат\n📎 Файлы:\n- [📎 r.txt](data:text/plain;base64,YQ==)",
    );
    const { container } = render(
      <MessageBubble message={msg} streaming={false} />,
    );
    // ReactMarkdown рендерит stripped, base64 не должен попасть на экран.
    expect(container.textContent).not.toContain("base64,YQ==");
    expect(container.textContent).toContain("результат");
    expect(container.textContent).toContain("r.txt");
  });

  it("рендерит live status-события, пока ответ ещё пустой", () => {
    render(
      <MessageBubble
        message={{
          role: "assistant",
          content: "",
          statusEvents: [
            {
              request_id: "req-1",
              stage: "llm_call",
              message: "Запускаю провайдера и жду ответ модели",
              provider: "codex-cli",
              model: "gpt-5.3-codex",
            },
          ],
        }}
        streaming={true}
      />,
    );
    expect(screen.getByTestId("message-status")).toBeInTheDocument();
    expect(screen.getAllByText(/жду ответ модели/i).length).toBeGreaterThan(0);
  });

  it("показывает статус как последовательную ленту переходов", () => {
    render(
      <MessageBubble
        message={{
          role: "assistant",
          content: "",
          statusEvents: [
            {
              request_id: "req-1",
              stage: "client_prepare",
              message: "Готовлю запрос",
              provider: "codex-cli",
              model: "gpt-5.3-codex",
              receivedAt: 1_000,
            },
            {
              request_id: "req-1",
              stage: "policy_checked",
              message: "Политика: allow_raw",
              provider: "codex-cli",
              model: "gpt-5.3-codex",
              receivedAt: 2_000,
            },
            {
              request_id: "req-1",
              stage: "llm_call",
              message: "Запускаю провайдера",
              provider: "codex-cli",
              model: "gpt-5.3-codex",
              receivedAt: 3_000,
            },
          ],
        }}
        streaming={true}
      />,
    );

    const block = screen.getByTestId("message-status");
    expect(block.textContent).toContain("шаг 3");
    expect(block.textContent).toContain("1. Подготовка запроса");
    expect(block.textContent).toContain("2. Проверка политики");
    expect(block.textContent).toContain("3. Вызов основной модели");
    expect(block.textContent).toContain("готово");
    expect(block.textContent).toContain("идёт");
    expect(block.textContent).toContain("сейчас");
  });

  it("сворачивает ход выполнения у завершённого ответа", () => {
    render(
      <MessageBubble
        message={{
          role: "assistant",
          content: "Готово",
          statusEvents: [
            {
              request_id: "req-1",
              stage: "client_prepare",
              message: "Готовлю запрос",
              provider: "codex-cli",
              model: "gpt-5.3-codex",
              receivedAt: 1_000,
            },
            {
              request_id: "req-1",
              stage: "done",
              message: "Ответ доставлен в чат",
              provider: "codex-cli",
              model: "gpt-5.3-codex",
              receivedAt: 2_000,
            },
          ],
        }}
        streaming={false}
      />,
    );

    expect(
      screen.getByRole("button", { name: /развернуть ход выполнения/i }),
    ).toHaveAttribute("aria-expanded", "false");
    expect(screen.getByTestId("message-status").textContent).toContain(
      "Ответ доставлен",
    );
    expect(screen.queryByTestId("message-status-steps")).toBeNull();
    expect(screen.queryByText("1. Подготовка запроса")).toBeNull();
  });

  it("даёт вручную раскрыть завершённый ход выполнения", () => {
    render(
      <MessageBubble
        message={{
          role: "assistant",
          content: "Готово",
          statusEvents: [
            {
              request_id: "req-1",
              stage: "client_prepare",
              message: "Готовлю запрос",
              provider: "codex-cli",
              model: "gpt-5.3-codex",
              receivedAt: 1_000,
            },
            {
              request_id: "req-1",
              stage: "done",
              message: "Ответ доставлен в чат",
              provider: "codex-cli",
              model: "gpt-5.3-codex",
              receivedAt: 2_000,
            },
          ],
        }}
        streaming={false}
      />,
    );

    fireEvent.click(
      screen.getByRole("button", { name: /развернуть ход выполнения/i }),
    );

    expect(
      screen.getByRole("button", { name: /свернуть ход выполнения/i }),
    ).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByTestId("message-status-steps")).toBeInTheDocument();
    expect(screen.getByText("1. Подготовка запроса")).toBeInTheDocument();
  });

  it("автоматически сворачивает ход выполнения после окончания стрима", () => {
    const baseMessage: Message = {
      role: "assistant",
      content: "",
      statusEvents: [
        {
          request_id: "req-1",
          stage: "llm_call",
          message: "Запускаю провайдера",
          provider: "codex-cli",
          model: "gpt-5.3-codex",
          receivedAt: 1_000,
        },
      ],
    };
    const { rerender } = render(
      <MessageBubble message={baseMessage} streaming={true} />,
    );
    expect(
      screen.getByRole("button", { name: /свернуть ход выполнения/i }),
    ).toHaveAttribute("aria-expanded", "true");

    rerender(
      <MessageBubble
        message={{
          ...baseMessage,
          content: "Ответ готов",
          statusEvents: [
            ...(baseMessage.statusEvents ?? []),
            {
              request_id: "req-1",
              stage: "done",
              message: "Ответ доставлен в чат",
              provider: "codex-cli",
              model: "gpt-5.3-codex",
              receivedAt: 2_000,
            },
          ],
        }}
        streaming={false}
      />,
    );

    expect(
      screen.getByRole("button", { name: /развернуть ход выполнения/i }),
    ).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByTestId("message-status-steps")).toBeNull();
  });
});
