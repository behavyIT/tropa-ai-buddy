create table if not exists sim_clock (
    id int primary key default 1 check (id = 1),
    sim_base timestamptz not null,
    set_at timestamptz not null
);

create table if not exists task_states (
    user_id text not null,
    task_id text not null,
    status text not null,
    updated_at timestamptz not null,
    done_at timestamptz,
    primary key (user_id, task_id)
);

create table if not exists user_access (
    user_id text not null,
    system text not null,
    status text not null,
    request_id text not null default '',
    action_id text not null default '',
    updated_at timestamptz not null,
    primary key (user_id, system)
);

create table if not exists actions (
    id text primary key,
    user_id text not null,
    type text not null,
    status text not null,
    title text not null,
    args jsonb not null default '{}',
    view jsonb not null default '{}',
    result jsonb,
    external_id text not null default '',
    external_status text not null default '',
    error text not null default '',
    created_at timestamptz not null,
    updated_at timestamptz not null
);
create index if not exists actions_user_idx on actions (user_id, created_at desc);
create index if not exists actions_ext_idx on actions (status, external_status);

create table if not exists messages (
    id text primary key,
    user_id text not null,
    role text not null,
    text text not null,
    payload jsonb not null default '{}',
    feedback smallint not null default 0,
    created_at timestamptz not null
);
create index if not exists messages_user_idx on messages (user_id, created_at);

create table if not exists chat_state (
    user_id text primary key,
    state jsonb not null,
    updated_at timestamptz not null
);

create table if not exists questions (
    id text primary key,
    asker_id text not null,
    mentor_id text not null,
    team_id text not null,
    text text not null,
    context text not null default '',
    urgent boolean not null default false,
    status text not null,
    answer text not null default '',
    answered_by text not null default '',
    answered_at timestamptz,
    card_id text not null default '',
    created_at timestamptz not null
);
create index if not exists questions_mentor_idx on questions (mentor_id, status);

create table if not exists question_askers (
    question_id text not null references questions (id) on delete cascade,
    user_id text not null,
    created_at timestamptz not null,
    primary key (question_id, user_id)
);

create table if not exists knowledge_cards (
    id text primary key,
    team_id text not null,
    title text not null,
    body text not null,
    author_id text not null,
    source_question_id text not null default '',
    created_at timestamptz not null
);

create table if not exists notifications (
    id text primary key,
    user_id text not null,
    kind text not null,
    title text not null,
    body text not null,
    ref text not null default '',
    read_at timestamptz,
    created_at timestamptz not null
);
create index if not exists notifications_user_idx on notifications (user_id, created_at desc);

create table if not exists events (
    id bigserial primary key,
    user_id text not null default '',
    team_id text not null default '',
    role text not null default '',
    name text not null,
    props jsonb not null default '{}',
    created_at timestamptz not null
);
create index if not exists events_team_idx on events (team_id, created_at);

create table if not exists feature_flags (
    key text not null,
    scope text not null,
    enabled boolean not null,
    updated_by text not null default '',
    updated_at timestamptz not null,
    primary key (key, scope)
);

create table if not exists cron_runs (
    job text not null,
    key text not null,
    ran_at timestamptz not null,
    primary key (job, key)
);
