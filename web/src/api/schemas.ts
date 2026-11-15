import { z } from 'zod'

/** Go encodes nil slices as null — normalise them to []. */
const arr = <T extends z.ZodTypeAny>(item: T) =>
  z.array(item).nullish().transform((v) => v ?? [])

export const PersonSchema = z.object({
  id: z.string(),
  name: z.string(),
  role: z.enum(['intern', 'mentor', 'lead', 'hrbp']),
  title: z.string(),
  team_id: z.string(),
  team_name: z.string().default(''),
  contact: z.string().default(''),
})
export type Person = z.infer<typeof PersonSchema>

export const FlagSchema = z.object({
  key: z.string(),
  description: z.string(),
  default: z.boolean(),
  enabled: z.boolean(),
  source: z.string(),
})
export type Flag = z.infer<typeof FlagSchema>

export const DemoSchema = z.object({
  enabled: z.boolean(),
  now: z.string(),
  today: z.string(),
  day: z.number(),
  corp_fault: z.boolean(),
})
export type Demo = z.infer<typeof DemoSchema>

export const MeSchema = z.object({
  user: PersonSchema,
  mentor: PersonSchema.optional(),
  lead: PersonSchema.optional(),
  flags: arr(FlagSchema),
  demo: DemoSchema,
  llm: z.string(),
})
export type Me = z.infer<typeof MeSchema>

export const StepSchema = z.object({
  task_id: z.string(),
  title: z.string(),
  status: z.string(),
  why: z.string().default(''),
  due: z.string().default(''),
})
export type Step = z.infer<typeof StepSchema>

export const TaskStatus = z.enum(['todo', 'in_progress', 'waiting', 'blocked', 'done'])
export type TaskStatus = z.infer<typeof TaskStatus>

export const TaskSchema = z.object({
  id: z.string(),
  title: z.string(),
  owner: z.enum(['intern', 'mentor', 'lead']),
  owner_name: z.string(),
  kind: z.enum(['manual', 'access', 'teachback']),
  status: TaskStatus,
  due_day: z.number(),
  due_date: z.string(),
  overdue: z.boolean(),
  blocked_by: arr(z.string()),
  waiting_for: z.string().optional(),
  doc: z.string().optional(),
  doc_title: z.string().optional(),
  checklist_ref: z.string().optional(),
  systems: arr(z.string()),
  topic: z.string().optional(),
  can_complete: z.boolean(),
  can_nudge: z.boolean(),
  can_start: z.boolean(),
})
export type Task = z.infer<typeof TaskSchema>

export const StageSchema = z.object({
  id: z.string(),
  title: z.string(),
  from_day: z.number(),
  to_day: z.number(),
  done: z.number(),
  total: z.number(),
  current: z.boolean(),
  tasks: arr(TaskSchema),
})
export type Stage = z.infer<typeof StageSchema>

export const RouteSchema = z.object({
  user_id: z.string(),
  user_name: z.string(),
  route_title: z.string(),
  day: z.number(),
  today: z.string(),
  done: z.number(),
  total: z.number(),
  percent: z.number(),
  overdue: z.number(),
  stages: arr(StageSchema),
  next: arr(StepSchema),
  waiting: arr(StepSchema),
  while_waiting: arr(StepSchema),
})
export type Route = z.infer<typeof RouteSchema>

export const ActionSchema = z.object({
  id: z.string(),
  type: z.string(),
  status: z.enum(['planned', 'executing', 'done', 'failed', 'cancelled']),
  title: z.string(),
  view: z.object({
    service: z.string(),
    fields: arr(z.object({ label: z.string(), value: z.string() })),
    warnings: arr(z.string()),
    confirm_label: z.string(),
    approver: z.string().optional(),
    permission: z.string(),
    note: z.string().optional(),
  }),
  result: z.unknown().optional(),
  external_id: z.string().optional(),
  external_status: z.string().optional(),
  error: z.string().optional(),
  created_at: z.string(),
  updated_at: z.string(),
})
export type Action = z.infer<typeof ActionSchema>

export const CitationSchema = z.object({
  n: z.number(),
  doc_id: z.string(),
  title: z.string(),
  kind: z.string(),
  snippet: z.string(),
  author: z.string().optional(),
  date: z.string().optional(),
})
export type Citation = z.infer<typeof CitationSchema>

const PersonCardSchema = z.object({
  id: z.string(),
  name: z.string(),
  title: z.string(),
  contact: z.string(),
  topic: z.string(),
  how: z.string().default(''),
  channel: z.string().optional(),
})

const ConceptSchema = z.object({ id: z.string(), label: z.string(), hint: z.string().optional() })

export const CardSchema = z.object({
  type: z.enum(['action', 'person', 'steps', 'teachback', 'requests']),
  action_id: z.string().optional(),
  person: PersonCardSchema.optional(),
  title: z.string().optional(),
  steps: arr(StepSchema),
  teachback: z
    .object({
      topic: z.string(),
      title: z.string(),
      score: z.number(),
      passed: z.boolean(),
      covered: arr(ConceptSchema),
      missing: arr(ConceptSchema),
      doc_id: z.string(),
    })
    .optional(),
  action_ids: arr(z.string()),
})
export type Card = z.infer<typeof CardSchema>

export const MessageSchema = z.object({
  id: z.string(),
  role: z.enum(['user', 'buddy']),
  text: z.string(),
  payload: z.object({
    intent: z.string().optional(),
    mode: z.string().optional(),
    citations: arr(CitationSchema),
    cards: arr(CardSchema),
    suggestions: arr(z.string()),
  }),
  feedback: z.number().default(0),
  created_at: z.string(),
})
export type Message = z.infer<typeof MessageSchema>

export const ChatSchema = z.object({ messages: arr(MessageSchema) })
export const ReplySchema = z.object({ user: MessageSchema, buddy: MessageSchema, actions: arr(ActionSchema) })
export type Reply = z.infer<typeof ReplySchema>
export const ActionsSchema = z.object({ actions: arr(ActionSchema) })

export const NotificationSchema = z.object({
  id: z.string(),
  kind: z.string(),
  title: z.string(),
  body: z.string(),
  ref: z.string().optional(),
  read_at: z.string().nullish(),
  created_at: z.string(),
})
export type Notification = z.infer<typeof NotificationSchema>
export const NotificationsSchema = z.object({ notifications: arr(NotificationSchema), unread: z.number() })

export const QuestionSchema = z.object({
  id: z.string(),
  asker_id: z.string(),
  text: z.string(),
  context: z.string().default(''),
  urgent: z.boolean(),
  status: z.string(),
  answer: z.string().optional(),
  card_id: z.string().optional(),
  created_at: z.string(),
  asker_names: arr(z.string()),
  ask_count: z.number(),
})
export type Question = z.infer<typeof QuestionSchema>
export const QuestionsSchema = z.object({ questions: arr(QuestionSchema) })

export const InternSchema = z.object({
  id: z.string(),
  name: z.string(),
  team: z.string(),
  day: z.number(),
  done: z.number(),
  total: z.number(),
  percent: z.number(),
  overdue: z.number(),
  waiting: arr(z.string()),
  next: z.string().default(''),
})
export type Intern = z.infer<typeof InternSchema>
export const TeamSchema = z.object({ interns: arr(InternSchema) })

export const ApprovalSchema = z.object({ action: ActionSchema, requester: z.string(), kind: z.string(), details: z.string() })
export type Approval = z.infer<typeof ApprovalSchema>
export const ApprovalsSchema = z.object({ approvals: arr(ApprovalSchema) })

export const InsightsSchema = z.object({
  scope: z.string(),
  interns: arr(InternSchema),
  metrics: z.object({
    chat_turns: z.number(),
    answered: z.number(),
    unanswered: z.number(),
    self_serve_rate: z.number(),
    helpful: z.number(),
    not_helpful: z.number(),
    escalated: z.number(),
    merged: z.number(),
    mentor_answers: z.number(),
    published: z.number(),
    actions_executed: z.number(),
    actions_failed: z.number(),
    teachbacks_passed: z.number(),
    tasks_done: z.number(),
    permission_denied: z.number(),
    sensitive_blocked: z.number(),
    mentor_minutes_saved: z.number(),
    llm_share: z.number(),
    avg_access_wait_hours: z.number(),
  }),
  hotspots: arr(
    z.object({ task_id: z.string(), title: z.string(), questions: z.number(), waiting: z.number(), overdue: z.number(), advice: z.string().default('') }),
  ),
  gaps: arr(z.object({ topic: z.string(), count: z.number(), source: z.string(), status: z.string() })),
  assumptions: arr(z.string()),
  synthetic_events: z.number(),
})
export type Insights = z.infer<typeof InsightsSchema>

export const FlagsSchema = z.object({ flags: arr(FlagSchema), team: z.string() })
export const OkSchema = z.object({}).passthrough()
