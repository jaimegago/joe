import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Badge } from '@/components/ui/badge';
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select';
import { setActiveModel, setCostLimit, setRunawayCeiling, setContextBudget } from '@/api/llm';
import { formatNanoCost, NANO_PER_UNIT } from '@/lib/usage';
import { describeLimit, type LimitSource } from '@/lib/llm-limits';
import type { LLMCostLimit, LLMRunawayCeiling, LLMContextBudget, LLMProvider } from '@/api/types';

function SourceBadge({ source }: { source: LimitSource }) {
  const variant = source === 'operator' ? 'success' : source === 'disabled' ? 'destructive' : 'secondary';
  const { label } = describeLimit(
    source === 'default'
      ? { state: 'backstop_fallback', effective: 1 }
      : { state: 'configured', effective: source === 'operator' ? 1 : 0 }
  );
  return <Badge variant={variant}>{label}</Badge>;
}

const CURRENCY_HINT = 'nano-units of the configured currency (1 unit = 1e9 nano)';

function CostLimitControl({ limit }: { limit: LLMCostLimit }) {
  const qc = useQueryClient();
  const [value, setValue] = useState('');
  const desc = describeLimit(limit);

  const mut = useMutation({
    mutationFn: (v: number) => setCostLimit(limit.window, v),
    onSuccess: () => {
      toast.success(`${limit.window} cost limit updated`);
      setValue('');
      void qc.invalidateQueries({ queryKey: ['llm-settings'] });
    },
    onError: (e: Error) => toast.error(e.message),
  });

  return (
    <Card data-testid={`limit-${limit.window}`}>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center justify-between text-sm capitalize">
          <span>{limit.window}</span>
          <SourceBadge source={desc.source} />
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <div>
          <span className="text-2xl font-semibold">
            {desc.source === 'disabled' ? 'No limit in force' : formatNanoCost(limit.effective, 'nano')}
          </span>
          <p className="text-xs text-muted-foreground">
            Effective enforced value{desc.source === 'default' ? ' (backstop default)' : ''}
          </p>
        </div>
        <form
          className="flex items-end gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            mut.mutate(Number(value));
          }}
        >
          <div className="flex-1 space-y-1">
            <Label htmlFor={`cost-${limit.window}`}>Set limit ({CURRENCY_HINT})</Label>
            <Input
              id={`cost-${limit.window}`}
              type="number"
              value={value}
              onChange={(e) => setValue(e.target.value)}
              placeholder={String(NANO_PER_UNIT)}
            />
          </div>
          <Button type="submit" size="sm" disabled={mut.isPending || value === ''}>
            Set
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}

function RunawayCeilingControl({ ceiling }: { ceiling: LLMRunawayCeiling }) {
  const qc = useQueryClient();
  const [value, setValue] = useState('');
  const desc = describeLimit(ceiling);

  const mut = useMutation({
    mutationFn: (v: number) => setRunawayCeiling(v),
    onSuccess: () => {
      toast.success('Runaway ceiling updated');
      setValue('');
      void qc.invalidateQueries({ queryKey: ['llm-settings'] });
    },
    onError: (e: Error) => toast.error(e.message),
  });

  return (
    <Card data-testid="limit-runaway">
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center justify-between text-sm">
          <span>Runaway ceiling</span>
          <SourceBadge source={desc.source} />
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <div>
          <span className="text-2xl font-semibold">
            {desc.source === 'disabled'
              ? 'No limit in force'
              : `${ceiling.effective.toLocaleString()} tokens`}
          </span>
          <p className="text-xs text-muted-foreground">
            Per-session token ceiling{desc.source === 'default' ? ' (backstop default)' : ''}
          </p>
        </div>
        <form
          className="flex items-end gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            mut.mutate(Number(value));
          }}
        >
          <div className="flex-1 space-y-1">
            <Label htmlFor="runaway-ceiling">Set ceiling (tokens)</Label>
            <Input
              id="runaway-ceiling"
              type="number"
              value={value}
              onChange={(e) => setValue(e.target.value)}
              placeholder="200000"
            />
          </div>
          <Button type="submit" size="sm" disabled={mut.isPending || value === ''}>
            Set
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}

// Client-side mirror of the backend's range check
// (internal/api/llmsettings.go handleSetContextBudget): a fraction must be
// greater than 0 and at most 1.0. Kept in lock-step with the 400 message so
// the operator sees the same rule the server enforces.
const CONTEXT_BUDGET_RANGE_ERROR = 'Fraction must be greater than 0 and at most 1.0';

function ContextBudgetControl({ budget }: { budget: LLMContextBudget }) {
  const qc = useQueryClient();
  const [value, setValue] = useState('');
  const [error, setError] = useState('');
  const desc = describeLimit(budget);

  const mut = useMutation({
    mutationFn: (v: number) => setContextBudget(v),
    onSuccess: () => {
      toast.success('Context budget updated');
      setValue('');
      setError('');
      void qc.invalidateQueries({ queryKey: ['llm-settings'] });
    },
    // Surface a backend rejection (e.g. a 400 from the range check) inline so
    // the operator sees the cause; also toast it to match the other controls.
    onError: (e: Error) => {
      setError(e.message);
      toast.error(e.message);
    },
  });

  return (
    <Card data-testid="limit-context-budget">
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center justify-between text-sm">
          <span>Context budget</span>
          <SourceBadge source={desc.source} />
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <div>
          <span className="text-2xl font-semibold">{budget.effective.toFixed(2)}</span>
          <p className="text-xs text-muted-foreground">
            Fraction of the model context window reserved for input{desc.source === 'default' ? ' (backstop default)' : ''}
          </p>
        </div>
        <form
          className="flex items-end gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            const n = Number(value);
            // Reject out-of-range input before any network call, matching the
            // backend's (0, 1.0] validation exactly.
            if (!Number.isFinite(n) || n <= 0 || n > 1) {
              setError(CONTEXT_BUDGET_RANGE_ERROR);
              return;
            }
            setError('');
            mut.mutate(n);
          }}
        >
          <div className="flex-1 space-y-1">
            <Label htmlFor="context-budget">Set fraction (0–1.0)</Label>
            <Input
              id="context-budget"
              type="number"
              step="0.05"
              value={value}
              onChange={(e) => setValue(e.target.value)}
              placeholder="0.70"
              disabled={mut.isPending}
            />
            {error && <p className="text-xs text-destructive">{error}</p>}
          </div>
          <Button type="submit" size="sm" disabled={mut.isPending || value === ''}>
            Set
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}

function ActiveModelControl({
  activeModel,
  models,
}: {
  activeModel: string;
  models: LLMProvider[];
}) {
  const qc = useQueryClient();
  const [selected, setSelected] = useState(activeModel);

  const mut = useMutation({
    mutationFn: (name: string) => setActiveModel(name),
    onSuccess: () => {
      toast.success('Active model updated');
      void qc.invalidateQueries({ queryKey: ['llm-settings'] });
      void qc.invalidateQueries({ queryKey: ['llm-providers'] });
    },
    onError: (e: Error) => toast.error(e.message),
  });

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm">Active model</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <p className="text-2xl font-semibold">{activeModel || '—'}</p>
        <form
          className="flex items-end gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            mut.mutate(selected);
          }}
        >
          <div className="flex-1 space-y-1">
            <Label>Change active model</Label>
            <Select value={selected} onValueChange={setSelected}>
              <SelectTrigger>
                <SelectValue placeholder="Select a model" />
              </SelectTrigger>
              <SelectContent>
                {models.map((m) => (
                  <SelectItem key={m.name} value={m.name}>
                    {m.name} ({m.provider})
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <Button type="submit" size="sm" disabled={mut.isPending || selected === '' || selected === activeModel}>
            Apply
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}

interface SettingsTabProps {
  activeModel: string;
  costLimits: LLMCostLimit[];
  runawayCeiling: LLMRunawayCeiling;
  contextBudget: LLMContextBudget;
  models: LLMProvider[];
}

export function SettingsTab({ activeModel, costLimits, runawayCeiling, contextBudget, models }: SettingsTabProps) {
  return (
    <div className="space-y-6">
      <ActiveModelControl activeModel={activeModel} models={models} />
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {costLimits.map((l) => (
          <CostLimitControl key={l.window} limit={l} />
        ))}
        <RunawayCeilingControl ceiling={runawayCeiling} />
        <ContextBudgetControl budget={contextBudget} />
      </div>
    </div>
  );
}
