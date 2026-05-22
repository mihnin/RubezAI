import "@testing-library/jest-dom/vitest";
import { afterEach } from "vitest";
import { cleanup } from "@testing-library/react";

// jsdom не реализует scrollTo (ChatPage скроллит ленту сообщений) — заглушка.
if (!Element.prototype.scrollTo) {
  Element.prototype.scrollTo = () => {};
}

afterEach(() => {
  cleanup();
  localStorage.clear();
});
