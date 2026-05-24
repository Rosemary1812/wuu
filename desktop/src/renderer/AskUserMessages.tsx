import { Check, MessageSquarePlus, X } from "lucide-react";
import { useMemo, useState } from "react";
import type { AskUserQuestion } from "../shared/protocol";

const ASK_USER_OTHER_VALUE = "__wuu_other__";

export type AskRequestState = {
  id: string;
  threadID?: string;
  questions: AskUserQuestion[];
};

export type AnsweredAskRequestState = AskRequestState & {
  answers: Record<string, string>;
  cancelled: boolean;
  turnID?: string;
};

export function AnsweredAskUserMessage({ request }: { request: AnsweredAskRequestState }): JSX.Element {
  return (
    <article className={`ask-message ask-message-answered${request.cancelled ? " cancelled" : ""}`} aria-live="polite">
      <div className="ask-header">
        <div className="ask-title">
          <Check size={17} />
          <span>{request.cancelled ? "已取消回答" : "你已回答"}</span>
        </div>
      </div>
      <div className="ask-body ask-answer-body">
        {request.cancelled ? (
          <div className="ask-answer-empty">这次提问没有提交答案。</div>
        ) : (
          request.questions.map((question) => {
            const answer = request.answers[question.question]?.trim();
            if (!answer) {
              return null;
            }
            return (
              <section key={question.question} className="ask-question ask-answer-question">
                <div className="ask-question-meta">
                  <div className="ask-chip">{question.header}</div>
                </div>
                <h3>{question.question}</h3>
                <div className="ask-answer-text">{answer}</div>
              </section>
            );
          })
        )}
      </div>
    </article>
  );
}

export function AskUserMessage({
  request,
  onCancel,
  onSubmit
}: {
  request: AskRequestState;
  onCancel: (request: AskRequestState) => Promise<void>;
  onSubmit: (request: AskRequestState, answers: Record<string, string>) => Promise<void>;
}): JSX.Element {
  const [answers, setAnswers] = useState<Record<string, string[]>>(() => {
    const initial: Record<string, string[]> = {};
    for (const question of request.questions) {
      initial[question.question] = [];
    }
    return initial;
  });
  const [otherAnswers, setOtherAnswers] = useState<Record<string, string>>({});
  const [submitting, setSubmitting] = useState(false);
  const flatAnswers = useMemo(() => {
    const output: Record<string, string> = {};
    for (const question of request.questions) {
      const selected = answers[question.question] ?? [];
      const other = otherAnswers[question.question]?.trim() ?? "";
      const values = selected.filter((label) => label !== ASK_USER_OTHER_VALUE);
      if (selected.includes(ASK_USER_OTHER_VALUE) && other) {
        values.push(other);
      }
      output[question.question] = values.join(", ");
    }
    return output;
  }, [answers, otherAnswers, request.questions]);
  const answeredCount = request.questions.filter((question) => flatAnswers[question.question]?.trim()).length;
  const allQuestionsAnswered = answeredCount === request.questions.length && request.questions.length > 0;

  function select(question: AskUserQuestion, label: string): void {
    setAnswers((current) => {
      const existing = current[question.question] ?? [];
      if (!question.multi_select) {
        return { ...current, [question.question]: [label] };
      }
      const next = existing.includes(label) ? existing.filter((item) => item !== label) : [...existing, label];
      return { ...current, [question.question]: next };
    });
  }

  function updateOtherAnswer(question: AskUserQuestion, value: string): void {
    setOtherAnswers((current) => ({ ...current, [question.question]: value }));
  }

  async function cancel(): Promise<void> {
    if (submitting) {
      return;
    }
    setSubmitting(true);
    try {
      await onCancel(request);
    } finally {
      setSubmitting(false);
    }
  }

  async function submit(): Promise<void> {
    if (submitting || !allQuestionsAnswered) {
      return;
    }
    setSubmitting(true);
    try {
      await onSubmit(request, flatAnswers);
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <article className="ask-message" aria-live="polite">
      <div className="ask-header">
        <div className="ask-title">
          <MessageSquarePlus size={17} />
          <span>需要你选择</span>
        </div>
        <button
          className="icon-button ask-dismiss"
          type="button"
          aria-label="取消这次提问"
          disabled={submitting}
          onClick={() => void cancel()}
        >
          <X size={17} />
        </button>
      </div>
      <div className="ask-body">
        {request.questions.map((question) => {
          const selectedAnswers = answers[question.question] ?? [];
          return (
            <section key={question.question} className="ask-question">
              <div className="ask-question-meta">
                <div className="ask-chip">{question.header}</div>
                {question.multi_select ? <div className="ask-chip secondary">可多选</div> : null}
              </div>
              <h3>{question.question}</h3>
              <div
                className="ask-options"
                role={question.multi_select ? "group" : "radiogroup"}
                aria-label={question.question}
              >
                {question.options.map((option) => {
                  const selected = selectedAnswers.includes(option.label);
                  return (
                    <button
                      key={option.label}
                      className={`ask-option ${selected ? "selected" : ""}`}
                      type="button"
                      role={question.multi_select ? "checkbox" : "radio"}
                      aria-checked={selected}
                      disabled={submitting}
                      onClick={() => select(question, option.label)}
                    >
                      <span className="ask-option-check" aria-hidden="true">
                        {selected ? <Check size={15} /> : null}
                      </span>
                      <span className="ask-option-copy">
                        <strong>{option.label}</strong>
                        {option.description ? <span>{option.description}</span> : null}
                        {option.preview ? <span className="ask-option-preview">{option.preview}</span> : null}
                      </span>
                    </button>
                  );
                })}
                <div className={`ask-other ${selectedAnswers.includes(ASK_USER_OTHER_VALUE) ? "selected" : ""}`}>
                  <button
                    className="ask-other-toggle"
                    type="button"
                    role={question.multi_select ? "checkbox" : "radio"}
                    aria-checked={selectedAnswers.includes(ASK_USER_OTHER_VALUE)}
                    disabled={submitting}
                    onClick={() => select(question, ASK_USER_OTHER_VALUE)}
                  >
                    <span className="ask-option-check" aria-hidden="true">
                      {selectedAnswers.includes(ASK_USER_OTHER_VALUE) ? <Check size={15} /> : null}
                    </span>
                    <span className="ask-option-copy">
                      <strong>其他</strong>
                    </span>
                  </button>
                  {selectedAnswers.includes(ASK_USER_OTHER_VALUE) ? (
                    <textarea
                      className="ask-other-input"
                      value={otherAnswers[question.question] ?? ""}
                      placeholder="输入答案"
                      rows={3}
                      disabled={submitting}
                      onChange={(event) => updateOtherAnswer(question, event.currentTarget.value)}
                    />
                  ) : null}
                </div>
              </div>
            </section>
          );
        })}
      </div>
      <div className="ask-footer">
        <span>{request.questions.length > 1 ? `${answeredCount}/${request.questions.length} 个已回答` : allQuestionsAnswered ? "已选择" : "等待你的选择"}</span>
        <div className="ask-actions">
          <button className="secondary-button" type="button" disabled={submitting} onClick={() => void cancel()}>
            取消
          </button>
          <button className="primary-button" type="button" disabled={submitting || !allQuestionsAnswered} onClick={() => void submit()}>
            提交
          </button>
        </div>
      </div>
    </article>
  );
}
