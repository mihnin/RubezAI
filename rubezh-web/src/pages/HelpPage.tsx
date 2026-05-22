import {
  MessageSquare,
  FileText,
  Shield,
  Cpu,
  ClipboardList,
  AlertTriangle,
  User,
  ShieldAlert,
  Settings,
  Eye,
  Lock,
  CloudOff,
} from "lucide-react";
import type { ComponentType } from "react";

/** HelpPage — справочный раздел: как устроена система, разделы, роли, решения.
 *  Статический контент (docs/ARCHITECTURE.md в человекочитаемом виде). */
export default function HelpPage() {
  return (
    <div className="p-8 max-w-4xl mx-auto space-y-10">
      <header>
        <h1 className="text-2xl font-semibold tracking-tight">Помощь</h1>
        <p className="text-sm text-slate-400 mt-1.5 leading-relaxed">
          «Рубеж ИИ» — on-prem шлюз, который позволяет сотрудникам безопасно
          пользоваться внешними и локальными LLM. Принцип:{" "}
          <b className="text-slate-200">
            сначала детерминированные правила, LLM лишь подсказывает, решение
            принимает policy engine, всё журналируется
          </b>
          . Персональные данные обезличиваются до выхода за контур; во внешние
          модели уходит только обезличенный текст.
        </p>
      </header>

      <Pipeline />

      <Section title="Разделы системы">
        <div className="grid sm:grid-cols-2 gap-3">
          {SECTIONS.map((s) => (
            <FeatureCard key={s.title} {...s} />
          ))}
        </div>
      </Section>

      <Section title="Сценарии по ролям">
        <div className="space-y-3">
          {STORIES.map((s) => (
            <Story key={s.role} {...s} />
          ))}
        </div>
      </Section>

      <Section title="Какие решения принимает система">
        <div className="space-y-2">
          {DECISIONS.map((d) => (
            <div
              key={d.code}
              className="flex items-start gap-3 bg-slate-900/50 border border-slate-800 rounded-lg px-4 py-2.5"
            >
              <span
                className={`text-xs font-mono px-2 py-0.5 rounded-full shrink-0 mt-0.5 ${d.cls}`}
              >
                {d.code}
              </span>
              <span className="text-sm text-slate-300">{d.text}</span>
            </div>
          ))}
        </div>
        <p className="text-xs text-slate-500 mt-3">
          Правила применяются сверху вниз, срабатывает первое подходящее. Если
          не подошло ни одно — запрос отклоняется (fail-closed).
        </p>
      </Section>

      <Section title="Уровни доверия моделей (trust level)">
        <div className="space-y-2">
          {TRUST.map((t) => (
            <div
              key={t.level}
              className="flex items-start gap-3 bg-slate-900/50 border border-slate-800 rounded-lg px-4 py-2.5"
            >
              <span className="text-xs font-mono text-cyan-300 shrink-0 mt-0.5 w-32">
                {t.level}
              </span>
              <span className="text-sm text-slate-300">{t.text}</span>
            </div>
          ))}
        </div>
      </Section>
    </div>
  );
}

function Pipeline() {
  const steps = [
    { icon: FileText, label: "Ввод текста / документа" },
    { icon: Shield, label: "Детекторы ПДн + локальная LLM-проверка" },
    { icon: Lock, label: "Обезличивание (псевдонимы)" },
    { icon: ShieldAlert, label: "Policy engine: allow / mask / deny" },
    { icon: CloudOff, label: "В LLM — только обезличенный текст" },
    { icon: Eye, label: "Ответ с псевдонимами → reveal по кнопке" },
  ];
  return (
    <Section title="Как это работает (конвейер)">
      <div className="flex flex-wrap items-stretch gap-2">
        {steps.map((s, i) => (
          <div key={s.label} className="flex items-center gap-2">
            <div className="bg-slate-900/60 border border-slate-800 rounded-lg px-3 py-2 flex items-center gap-2 text-xs text-slate-300">
              <s.icon className="w-4 h-4 text-cyan-400 shrink-0" strokeWidth={2} />
              {s.label}
            </div>
            {i < steps.length - 1 && (
              <span className="text-slate-600 text-sm">→</span>
            )}
          </div>
        ))}
      </div>
    </Section>
  );
}

function Section({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <section>
      <h2 className="text-sm font-semibold uppercase tracking-wider text-slate-400 mb-3">
        {title}
      </h2>
      {children}
    </section>
  );
}

function FeatureCard({
  icon: Icon,
  title,
  what,
  why,
}: {
  icon: ComponentType<{ className?: string; strokeWidth?: number }>;
  title: string;
  what: string;
  why: string;
}) {
  return (
    <div className="bg-slate-900/50 border border-slate-800 rounded-xl p-4">
      <div className="flex items-center gap-2 mb-2">
        <div className="w-8 h-8 rounded-lg bg-cyan-500/10 flex items-center justify-center">
          <Icon className="w-4 h-4 text-cyan-300" strokeWidth={2} />
        </div>
        <h3 className="font-medium">{title}</h3>
      </div>
      <p className="text-sm text-slate-300">{what}</p>
      <p className="text-xs text-slate-500 mt-1.5">
        <b className="text-slate-400">Зачем:</b> {why}
      </p>
    </div>
  );
}

function Story({
  icon: Icon,
  role,
  color,
  steps,
}: {
  icon: ComponentType<{ className?: string; strokeWidth?: number }>;
  role: string;
  color: string;
  steps: string[];
}) {
  return (
    <div className="bg-slate-900/50 border border-slate-800 rounded-xl p-5">
      <div className="flex items-center gap-2 mb-3">
        <div
          className={`w-8 h-8 rounded-lg flex items-center justify-center ${color}`}
        >
          <Icon className="w-4 h-4" strokeWidth={2} />
        </div>
        <h3 className="font-semibold">{role}</h3>
      </div>
      <ol className="space-y-1.5">
        {steps.map((s, i) => (
          <li key={i} className="flex gap-2.5 text-sm text-slate-300">
            <span className="text-cyan-400 font-mono text-xs mt-0.5 shrink-0">
              {i + 1}.
            </span>
            <span>{s}</span>
          </li>
        ))}
      </ol>
    </div>
  );
}

const SECTIONS = [
  {
    icon: MessageSquare,
    title: "Чат",
    what: "Диалог с LLM: ввод текста или документа, обезличивание, предпросмотр перед облаком, ответ с псевдонимами и кнопка «Показать реальные данные».",
    why: "Легальный безопасный канал вместо теневого копипаста в публичный ChatGPT — raw-ПДн не покидает периметр.",
  },
  {
    icon: FileText,
    title: "Документы",
    what: "Загрузка договоров/выписок, парсинг и обезличивание. Скачивание оригинала и обезличенной версии, статистика по ПДн.",
    why: "Основной носитель ПДн — документы. Обезличиваем целиком до отправки в LLM или подрядчику.",
  },
  {
    icon: Shield,
    title: "Политики",
    what: "Правила решения allow/mask/deny/escalate. В MVP только просмотр; изменения — через API после согласования с ИБ.",
    why: "Политика безопасности «на бумаге» становится исполняемым кодом — для регулятора видно, какое правило и почему сработало.",
  },
  {
    icon: Cpu,
    title: "Модели",
    what: "Провайдеры LLM (облачные/локальные), уровни доверия, API-ключи (шифруются), включение/выключение без перезапуска.",
    why: "Trust level — вход в policy engine: внешняя модель никогда не получит raw, локальная в периметре может.",
  },
  {
    icon: ClipboardList,
    title: "Аудит",
    what: "Неизменяемый журнал всех решений и действий: кто, что, какое правило, какой риск. Экспорт CSV.",
    why: "Доказательная база для регулятора и расследований; записи нельзя изменить или удалить.",
  },
  {
    icon: AlertTriangle,
    title: "Инциденты",
    what: "Авто- и ручные инциденты при deny/escalate/утечке. Расследование, назначение, резолюция, заметки.",
    why: "Замыкает петлю: обнаружили риск → человек разобрался. Эскалации не повисают.",
  },
];

const STORIES = [
  {
    icon: User,
    role: "Сотрудник (user)",
    color: "bg-cyan-500/15 text-cyan-300",
    steps: [
      "Открывает «Чат», выбирает модель (облачную DeepSeek/GPT или локальную).",
      "Пишет вопрос с реальными данными клиента или прикрепляет договор скрепкой 📎.",
      "Видит предупреждение и обезличенный текст: «ФИО_001, паспорт ПАСПОРТ_001…» — данные уйдут в облако замаскированными.",
      "Нажимает «Отправить в облако» — модель отвечает по псевдонимам.",
      "При необходимости жмёт «Показать реальные данные» — псевдонимы заменяются настоящими (локально, действие пишется в аудит).",
    ],
  },
  {
    icon: ShieldAlert,
    role: "Офицер ИБ (security_officer)",
    color: "bg-amber-500/15 text-amber-300",
    steps: [
      "Смотрит «Политики» — какие правила действуют (секреты → запрет, критический риск → эскалация и т. д.).",
      "Получает «Инциденты» при попытке отправить секрет или утечке — с привязкой к событию аудита.",
      "Расследует: меняет статус (в работе → закрыт), пишет резолюцию и заметки.",
      "Через «Аудит» восстанавливает полную картину: кто, когда, что отправлял и какое решение принято.",
      "Согласует изменения политик и список допустимых моделей с администратором.",
    ],
  },
  {
    icon: Settings,
    role: "Администратор (admin)",
    color: "bg-emerald-500/15 text-emerald-300",
    steps: [
      "В «Моделях» подключает провайдеров: облачные (DeepSeek/GPT/Claude/Gemini/Grok) и локальные.",
      "Вводит API-ключ провайдера (хранится зашифрованным) и задаёт уровень доверия.",
      "Включает/выключает провайдера — изменения видны в чате сразу, без перезапуска.",
      "Удаляет ненужных провайдеров (если использовались в истории — система предложит выключить вместо удаления).",
      "Управляет доступностью моделей; для облачных гарантируется отправка только обезличенного текста.",
    ],
  },
];

const DECISIONS = [
  {
    code: "allow_raw",
    cls: "bg-emerald-500/15 text-emerald-300",
    text: "Чувствительных данных нет (или модель локальная и доверенная) — текст уходит как есть.",
  },
  {
    code: "allow_masked",
    cls: "bg-cyan-500/15 text-cyan-300",
    text: "Найдены ПДн — во внешнюю модель уходит обезличенный текст; в ответе псевдонимы можно раскрыть по кнопке.",
  },
  {
    code: "allow_summary_only",
    cls: "bg-sky-500/15 text-sky-300",
    text: "Высокий риск при внешней модели — допускается только краткая выжимка без деталей.",
  },
  {
    code: "escalate",
    cls: "bg-amber-500/15 text-amber-300",
    text: "Критический уровень риска — требуется решение службы ИБ, создаётся инцидент.",
  },
  {
    code: "deny",
    cls: "bg-red-500/15 text-red-300",
    text: "Обнаружены секреты (пароли, ключи, токены, CVC) — отправка в любую LLM запрещена.",
  },
];

const TRUST = [
  {
    level: "external",
    text: "Облачная модель за пределами периметра (DeepSeek/GPT/Claude/Gemini/Grok). Получает ТОЛЬКО обезличенный текст.",
  },
  {
    level: "trusted_local",
    text: "Локальная модель в периметре (LM Studio/Ollama/vLLM). Может получить raw; используется и как доп. проверка обезличивания.",
  },
  {
    level: "russian_cloud / on_prem",
    text: "Российское облако / собственная инфраструктура — промежуточные уровни доверия для гибких политик.",
  },
];
