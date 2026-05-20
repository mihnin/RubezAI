import { Component, type ReactNode, type ErrorInfo } from "react";

interface State {
  error: Error | null;
}

/** ErrorBoundary — глобальный фоллбек для React-исключений.
 * Зон-кат для отображения ошибок Zod .parse() и runtime багов. */
export class ErrorBoundary extends Component<{ children: ReactNode }, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // В production здесь — sentry/loki; в dev — консоль.
    console.error("[ErrorBoundary]", error, info.componentStack);
  }

  reset = () => this.setState({ error: null });

  render() {
    if (!this.state.error) return this.props.children;
    return (
      <div className="min-h-screen flex items-center justify-center bg-slate-950 text-slate-50">
        <div className="max-w-lg p-6 bg-slate-900 border border-red-700 rounded-lg">
          <h1 className="text-lg font-semibold text-red-300 mb-2">
            Что-то пошло не так
          </h1>
          <p className="text-sm text-slate-400 mb-4">
            {this.state.error.message}
          </p>
          <div className="flex gap-2">
            <button
              onClick={this.reset}
              className="px-3 py-1.5 text-sm rounded bg-slate-800 hover:bg-slate-700"
            >
              Повторить
            </button>
            <button
              onClick={() => (window.location.href = "/")}
              className="px-3 py-1.5 text-sm rounded bg-cyan-500 text-slate-950"
            >
              На главную
            </button>
          </div>
        </div>
      </div>
    );
  }
}
