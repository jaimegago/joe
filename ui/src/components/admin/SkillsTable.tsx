import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { EmptyState } from '@/components/common/EmptyState';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { reloadSkills, approveSkill, rejectSkill } from '@/api/skills';
import type { SkillsListResponse, SkillStatusEntry } from '@/api/types';
import { Puzzle, RotateCw } from 'lucide-react';

const QUERY_KEY = ['skills'] as const;

// Source renders where a skill came from: its repo, the ref it tracks (branch /
// tag, when pinned), and the short commit it resolved to. This is the "from
// where" half of the surface — the part an operator checks to trust a skill.
function Source({ entry }: { entry: SkillStatusEntry }) {
  return (
    <div className="space-y-0.5">
      <div className="font-mono text-sm break-all">{entry.repo}</div>
      <div className="text-muted-foreground flex gap-2 text-xs">
        {entry.ref && <span>ref: {entry.ref}</span>}
        {entry.commit && <span className="font-mono">{entry.commit.slice(0, 12)}</span>}
      </div>
    </div>
  );
}

// SkillsTable is the operator surface for Joe's loaded skills: what is active,
// where each came from, and what is held in quarantine awaiting a decision.
// "Full management" — it can reload the registry and approve/reject pending
// installs — but it never shows or edits a skill's body.
//
// Admin-gated by its host: the only caller is the Admin page, whose whole route
// is behind <RequireAdmin>, so a non-admin never reaches this control.
export function SkillsTable({ data }: { data: SkillsListResponse }) {
  const qc = useQueryClient();
  const [rejecting, setRejecting] = useState<string | null>(null);

  const reloadMut = useMutation({
    mutationFn: reloadSkills,
    onSuccess: (res) => {
      const changed =
        (res.added?.length ?? 0) + (res.removed?.length ?? 0) + (res.updated?.length ?? 0);
      toast.success(
        changed === 0
          ? `Reloaded — no change (${res.after} skill${res.after === 1 ? '' : 's'})`
          : `Reloaded — ${res.before} → ${res.after} skills`
      );
      void qc.invalidateQueries({ queryKey: QUERY_KEY });
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const approveMut = useMutation({
    mutationFn: (name: string) => approveSkill(name),
    onSuccess: (res) => {
      toast.success(`Approved "${res.name}" — reload to load it into the router`);
      void qc.invalidateQueries({ queryKey: QUERY_KEY });
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const rejectMut = useMutation({
    mutationFn: (name: string) => rejectSkill(name),
    onSuccess: (res) => {
      toast.success(`Rejected "${res.name}" — removed from disk`);
      void qc.invalidateQueries({ queryKey: QUERY_KEY });
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const busy = reloadMut.isPending || approveMut.isPending || rejectMut.isPending;

  return (
    <div className="space-y-8">
      <div className="flex items-start justify-between gap-4">
        <p className="text-muted-foreground max-w-2xl text-sm">
          Skills are senior-SRE judgment frames Joe loads from git repositories and routes into its
          reasoning. Active skills are loaded now; quarantined skills are on disk but held until an
          operator approves them. Install and update skills with the <code>joe skills</code> CLI.
        </p>
        <Button
          variant="outline"
          size="sm"
          disabled={busy}
          onClick={() => reloadMut.mutate()}
          className="shrink-0"
        >
          <RotateCw className={`mr-1 h-4 w-4 ${reloadMut.isPending ? 'animate-spin' : ''}`} />
          {reloadMut.isPending ? 'Reloading…' : 'Reload'}
        </Button>
      </div>

      <section className="space-y-3">
        <h3 className="text-sm font-medium">Active</h3>
        {data.active.length === 0 ? (
          <EmptyState
            icon={Puzzle}
            title="No active skills"
            description="Install skills with `joe skills install <repo-url>`, then reload."
          />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Skill</TableHead>
                <TableHead>Source</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.active.map((s) => (
                <TableRow key={`${s.repo}/${s.name}`}>
                  <TableCell className="align-top">
                    <div className="font-medium">{s.name}</div>
                    {s.description && (
                      <div className="text-muted-foreground max-w-md text-sm">{s.description}</div>
                    )}
                  </TableCell>
                  <TableCell className="align-top">
                    <Source entry={s} />
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </section>

      {data.quarantined.length > 0 && (
        <section className="space-y-3">
          <div className="flex items-center gap-2">
            <h3 className="text-sm font-medium">Quarantined</h3>
            <Badge variant="secondary">{data.quarantined.length} pending</Badge>
          </div>
          <p className="text-muted-foreground max-w-2xl text-sm">
            These installs came from an untrusted source and are held off the router until you
            approve them. Review the source, then approve to keep or reject to delete from disk.
          </p>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Skill</TableHead>
                <TableHead>Source</TableHead>
                <TableHead>Reason</TableHead>
                <TableHead></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.quarantined.map((s) => (
                <TableRow key={`${s.repo}/${s.name}`}>
                  <TableCell className="align-top font-medium">{s.name}</TableCell>
                  <TableCell className="align-top">
                    <Source entry={s} />
                  </TableCell>
                  <TableCell className="text-muted-foreground align-top text-sm">
                    {s.quarantine_reason ?? '—'}
                  </TableCell>
                  <TableCell className="align-top">
                    <div className="flex justify-end gap-2">
                      <Button
                        variant="outline"
                        size="sm"
                        disabled={busy}
                        onClick={() => approveMut.mutate(s.name)}
                      >
                        Approve
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={busy}
                        onClick={() => setRejecting(s.name)}
                      >
                        Reject
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </section>
      )}

      <ConfirmDialog
        open={rejecting !== null}
        onOpenChange={(open) => !open && setRejecting(null)}
        title="Reject skill?"
        description={`This deletes "${rejecting ?? ''}" from disk. Reinstall it with the joe skills CLI to recover.`}
        confirmLabel="Reject"
        variant="destructive"
        onConfirm={() => {
          if (rejecting) rejectMut.mutate(rejecting);
          setRejecting(null);
        }}
      />
    </div>
  );
}
