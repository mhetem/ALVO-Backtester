<script lang="ts">
  import RuleNode from './RuleNode.svelte';
  import {
    blankLevel,
    blankRule,
    countsBars,
    flagged,
    namedOperand,
    operandCount,
    BRACKETS,
    COMPARATORS,
    COMPARATOR_LABELS,
    LEVEL_TYPES,
    PRICE_FIELDS,
    type BracketKind,
    type Comparator,
    type OperandDraft,
    type RuleDraft,
  } from './strategy';

  type Props = {
    rule: RuleDraft;
    names: string[];
    at: string;
    fault: string;
    brackets: boolean;
    onChange: (next: RuleDraft) => void;
    onRemove: (() => void) | null;
  };

  let { rule, names, at, fault, brackets, onChange, onRemove }: Props = $props();

  function operatorOf(node: RuleDraft): string {
    return node.kind === 'not' ? 'not' : node.op;
  }

  const operator = $derived(operatorOf(rule));
  const here = $derived(flagged(fault, at));
  const leaf = $derived(rule.kind === 'compare' || rule.kind === 'bracket');

  function childPath(key: string, index: number | null): string {
    return index === null ? `${at}/${key}` : `${at}/${key}/${index}`;
  }

  function pick(next: string) {
    if (next === 'all' || next === 'any') {
      onChange(
        rule.kind === 'group' ? { ...rule, op: next } : { kind: 'group', op: next, children: [rule] },
      );
      return;
    }

    if (next === 'not') {
      onChange(rule.kind === 'not' ? rule : { kind: 'not', child: rule });
      return;
    }

    if ((BRACKETS as readonly string[]).includes(next)) {
      onChange({ kind: 'bracket', op: next as BracketKind, level: blankLevel() });
      return;
    }

    const op = next as Comparator;
    const held = rule.kind === 'compare' ? rule.operands : blankRule(names).operands;

    onChange({ kind: 'compare', op, operands: fit(held, op) });
  }

  function fit(operands: OperandDraft[], op: Comparator): OperandDraft[] {
    const wanted = operandCount(op);
    const next = operands.slice(0, wanted);

    while (next.length < wanted) {
      next.push({ kind: 'number', value: countsBars(op) && next.length === 1 ? 1 : 0 });
    }
    if (countsBars(op) && next[1].kind !== 'number') {
      next[1] = { kind: 'number', value: 1 };
    }

    return next;
  }

  function setOperand(index: number, operand: OperandDraft) {
    if (rule.kind !== 'compare') {
      return;
    }
    onChange({ ...rule, operands: rule.operands.map((held, i) => (i === index ? operand : held)) });
  }

  function chooseOperand(index: number, choice: string) {
    const held = rule.kind === 'compare' ? rule.operands[index] : null;
    const back = held && held.kind !== 'number' ? held.back : 0;

    if (choice === 'number') {
      setOperand(index, { kind: 'number', value: 0 });
      return;
    }
    setOperand(index, namedOperand(choice, back));
  }

  function operandChoice(operand: OperandDraft): string {
    if (operand.kind === 'number') {
      return 'number';
    }
    return operand.kind === 'input' ? operand.name : operand.field;
  }

  function setChild(index: number, next: RuleDraft) {
    if (rule.kind !== 'group') {
      return;
    }
    onChange({ ...rule, children: rule.children.map((child, i) => (i === index ? next : child)) });
  }

  function addChild() {
    if (rule.kind !== 'group') {
      return;
    }
    onChange({ ...rule, children: [...rule.children, blankRule(names)] });
  }

  function dropChild(index: number) {
    if (rule.kind !== 'group') {
      return;
    }
    onChange({ ...rule, children: rule.children.filter((_, i) => i !== index) });
  }

  function setLevel(field: 'type' | 'value' | 'period' | 'mult', raw: string) {
    if (rule.kind !== 'bracket') {
      return;
    }
    if (field === 'type') {
      onChange({ ...rule, level: { ...rule.level, type: raw === 'atr' ? 'atr' : 'pct' } });
      return;
    }

    const value = Number(raw);
    if (!Number.isFinite(value)) {
      return;
    }

    const level = { ...rule.level };
    level[field] = value;
    onChange({ ...rule, level });
  }
</script>

<div class="node" class:flagged={here} class:leafy={leaf}>
  <div class="row">
    <select
      value={operator}
      onchange={(event) => pick(event.currentTarget.value)}
      aria-label="Rule operator"
    >
      <optgroup label="Compare">
        {#each COMPARATORS as op (op)}
          <option value={op}>{COMPARATOR_LABELS[op]}</option>
        {/each}
      </optgroup>
      <optgroup label="Combine">
        <option value="all">all of</option>
        <option value="any">any of</option>
        <option value="not">not</option>
      </optgroup>
      {#if brackets}
        <optgroup label="Attach to the position">
          <option value="stop_loss">stop loss</option>
          <option value="take_profit">take profit</option>
        </optgroup>
      {/if}
    </select>

    {#if rule.kind === 'compare'}
      {#each rule.operands as operand, index (index)}
        {#if countsBars(rule.op) && index === 1}
          <label class="bars">
            <input
              type="number"
              min="1"
              step="1"
              value={operand.kind === 'number' ? operand.value : 1}
              oninput={(event) =>
                setOperand(index, { kind: 'number', value: Number(event.currentTarget.value) })}
            />
            <span>bars</span>
          </label>
        {:else}
          <select
            value={operandChoice(operand)}
            onchange={(event) => chooseOperand(index, event.currentTarget.value)}
            aria-label="Operand"
          >
            {#if names.length > 0}
              <optgroup label="Inputs">
                {#each names as name (name)}
                  <option value={name}>{name}</option>
                {/each}
              </optgroup>
            {/if}
            <optgroup label="Price">
              {#each PRICE_FIELDS as field (field)}
                <option value={field}>{field}</option>
              {/each}
            </optgroup>
            <optgroup label="Literal">
              <option value="number">a number</option>
            </optgroup>
          </select>

          {#if operand.kind === 'number'}
            <input
              type="number"
              step="any"
              value={operand.value}
              oninput={(event) =>
                setOperand(index, { kind: 'number', value: Number(event.currentTarget.value) })}
              aria-label="Value"
            />
          {:else}
            <label class="back" title="How many bars back to read this operand">
              <input
                type="number"
                min="0"
                step="1"
                value={operand.back}
                oninput={(event) =>
                  setOperand(
                    index,
                    namedOperand(
                      operandChoice(operand),
                      Math.max(0, Number(event.currentTarget.value) || 0),
                    ),
                  )}
              />
              <span>back</span>
            </label>
          {/if}
        {/if}
      {/each}
    {/if}

    {#if rule.kind === 'bracket'}
      <select
        value={rule.level.type}
        onchange={(event) => setLevel('type', event.currentTarget.value)}
        aria-label="Level type"
      >
        {#each LEVEL_TYPES as type (type)}
          <option value={type}>{type}</option>
        {/each}
      </select>

      {#if rule.level.type === 'pct'}
        <label class="back">
          <input
            type="number"
            min="0"
            max="1"
            step="0.005"
            value={rule.level.value}
            oninput={(event) => setLevel('value', event.currentTarget.value)}
          />
          <span>of entry</span>
        </label>
      {:else}
        <label class="back">
          <input
            type="number"
            min="1"
            step="1"
            value={rule.level.period}
            oninput={(event) => setLevel('period', event.currentTarget.value)}
          />
          <span>period</span>
        </label>
        <label class="back">
          <input
            type="number"
            min="0.1"
            step="0.1"
            value={rule.level.mult}
            oninput={(event) => setLevel('mult', event.currentTarget.value)}
          />
          <span>×</span>
        </label>
      {/if}
    {/if}

    <span class="spacer"></span>

    {#if onRemove}
      <button type="button" class="drop" onclick={onRemove} aria-label="Remove this rule">×</button>
    {/if}
  </div>

  {#if rule.kind === 'not'}
    <div class="children">
      <RuleNode
        rule={rule.child}
        {names}
        {fault}
        brackets={false}
        at={childPath('not', null)}
        onChange={(next) => onChange({ kind: 'not', child: next })}
        onRemove={null}
      />
    </div>
  {/if}

  {#if rule.kind === 'group'}
    <div class="children">
      {#each rule.children as child, index (index)}
        <RuleNode
          rule={child}
          {names}
          {fault}
          brackets={brackets && rule.op === 'any'}
          at={childPath(rule.op, index)}
          onChange={(next) => setChild(index, next)}
          onRemove={() => dropChild(index)}
        />
      {/each}

      <button type="button" class="add" onclick={addChild}>Add a condition</button>
    </div>
  {/if}
</div>

<style>
  .node {
    border: 1px solid var(--line);
    border-radius: 0.35rem;
    background: var(--bg);
  }

  .node.flagged {
    border-color: var(--bad);
    box-shadow: 0 0 0 2px color-mix(in srgb, var(--bad) 18%, transparent);
  }

  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.35rem;
    padding: 0.4rem 0.45rem;
  }

  .children {
    display: flex;
    flex-direction: column;
    gap: 0.35rem;
    padding: 0 0.45rem 0.45rem 1.1rem;
    border-left: 1px solid var(--line);
    margin-left: 0.7rem;
  }

  select,
  input {
    font: inherit;
    font-size: 0.78rem;
    color: inherit;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: 0.25rem;
    padding: 0.18rem 0.35rem;
  }

  select:focus,
  input:focus {
    outline: none;
    border-color: var(--accent);
  }

  input[type='number'] {
    width: 4.5rem;
    font-variant-numeric: tabular-nums;
  }

  .back,
  .bars {
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
  }

  .back span,
  .bars span {
    color: var(--muted);
    font-size: 0.68rem;
    letter-spacing: 0.04em;
  }

  .spacer {
    flex: 1;
  }

  .drop {
    font: inherit;
    font-size: 0.95rem;
    line-height: 1;
    color: var(--muted);
    background: none;
    border: 0;
    padding: 0.1rem 0.3rem;
    cursor: pointer;
  }

  .drop:hover {
    color: var(--bad);
  }

  .add {
    align-self: flex-start;
    font: inherit;
    font-size: 0.72rem;
    font-weight: 500;
    color: var(--accent);
    background: none;
    border: 1px dashed var(--line);
    border-radius: 0.25rem;
    padding: 0.2rem 0.55rem;
    cursor: pointer;
  }

  .add:hover {
    border-color: var(--accent);
  }
</style>
