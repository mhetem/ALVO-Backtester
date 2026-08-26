export const HELP_TOPICS = ['strategies', 'backtests', 'sweeps', 'data'] as const;

export type HelpTopic = (typeof HELP_TOPICS)[number];

export const HELP_LABELS: Record<HelpTopic, string> = {
  strategies: 'Strategies',
  backtests: 'Backtests',
  sweeps: 'Sweeps',
  data: 'The data',
};
