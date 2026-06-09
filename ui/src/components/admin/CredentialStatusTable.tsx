import { useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { probeCredential, fetchCredentialStderr } from '@/api/credentialStatus';
import type { CredentialStatusEntry, CredentialDiagnostic, CredentialProbeResponse } from '@/api/types';
import { Copy } from 'lucide-react';

// StageBadge maps a staged diagnostic to a single status badge. The four cases
// are deliberate and distinct (D-0026 §"stage enum"):
//   - connectivity-probed + ok → success (connectivity proven against the backend)
//   - mint-succeeded + ok       → the LAZY state: "minted, not yet proven". This is
//     a LEGITIMATE terminal success, rendered neutral — never a warning/failure.
//   - !ok                       → failure, named at the stage it stopped
//   - any residual ok state     → neutral
function StageBadge({ diagnostic }: { diagnostic: CredentialDiagnostic }) {
  const { stage, ok } = diagnostic;
  if (ok && stage === 'connectivity-probed') {
    return (
      <Badge className="border-transparent bg-green-100 text-green-800 hover:bg-green-100/80">
        Connectivity verified
      </Badge>
    );
  }
  if (ok && stage === 'mint-succeeded') {
    // Lazy-connectivity: minted, first real call will prove it. Neutral, not a warning.
    return <Badge variant="secondary">Minted · not yet proven</Badge>;
  }
  if (!ok) {
    return <Badge variant="destructive">Failed at {stage}</Badge>;
  }
  return <Badge variant="secondary">{stage}</Badge>;
}

// CredentialStatusRow renders one component's passive descriptor and, after a
// deliberate probe, its staged connectivity result. The captured plugin stderr is
// fetched and shown ONLY when the operator expands it — it never loads with the
// row and never rides along the probe response.
function CredentialStatusRow({ entry }: { entry: CredentialStatusEntry }) {
  const [probe, setProbe] = useState<CredentialProbeResponse | null>(null);
  const [stderr, setStderr] = useState<string | null>(null);

  const probeMut = useMutation({
    mutationFn: () => probeCredential(entry.component_id),
    onSuccess: (res) => {
      setProbe(res);
      setStderr(null); // a fresh probe invalidates any previously-shown plugin output
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const stderrMut = useMutation({
    mutationFn: () => fetchCredentialStderr(entry.component_id),
    onSuccess: (s) => setStderr(s),
    onError: (e: Error) => toast.error(e.message),
  });

  const d = entry.descriptor;
  const diag = probe?.diagnostic;
  // The detail row appears only when there is something to act on: a failure
  // reason to read, or captured plugin output to optionally reveal. Success and
  // the lazy "minted, not yet proven" state need no extra row.
  const showDetail = probe !== null && (!probe.diagnostic.ok || probe.stderr_available);

  return (
    <>
      <TableRow>
        <TableCell className="font-mono text-sm">{entry.component_id}</TableCell>
        <TableCell>
          <Badge variant="secondary">{entry.type}</Badge>
        </TableCell>
        <TableCell>
          {d ? (
            <span className="text-sm">{d.provider}</span>
          ) : (
            <span className="text-muted-foreground text-sm" title={entry.error}>
              unconfigured
            </span>
          )}
        </TableCell>
        <TableCell className="text-muted-foreground text-sm">{d?.context ?? d?.audience ?? '—'}</TableCell>
        <TableCell className="text-muted-foreground text-sm">
          {d?.expires_at ? new Date(d.expires_at).toLocaleString() : '—'}
        </TableCell>
        <TableCell>
          {diag ? (
            <StageBadge diagnostic={diag} />
          ) : (
            <span className="text-muted-foreground text-sm">Not probed</span>
          )}
        </TableCell>
        <TableCell>
          <div className="flex justify-end">
            <Button
              variant="outline"
              size="sm"
              disabled={probeMut.isPending}
              onClick={() => probeMut.mutate()}
            >
              {probeMut.isPending ? 'Probing…' : 'Probe now'}
            </Button>
          </div>
        </TableCell>
      </TableRow>

      {showDetail && probe && (
        <TableRow>
          <TableCell colSpan={7} className="bg-muted/30">
            <div className="space-y-2 py-1 text-sm">
              {probe.diagnostic.reason && (
                <p className="text-destructive">{probe.diagnostic.reason}</p>
              )}
              {probe.stderr_available &&
                (stderr === null ? (
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={stderrMut.isPending}
                    onClick={() => stderrMut.mutate()}
                  >
                    {stderrMut.isPending ? 'Loading…' : 'Show plugin output'}
                  </Button>
                ) : (
                  <div className="space-y-1">
                    <div className="flex items-center justify-between">
                      <span className="text-xs text-muted-foreground">
                        Plugin output — untrusted, may contain secrets
                      </span>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => void navigator.clipboard.writeText(stderr)}
                      >
                        <Copy className="mr-1 h-3 w-3" />
                        Copy
                      </Button>
                    </div>
                    <pre className="max-h-48 overflow-auto whitespace-pre-wrap break-all rounded border bg-background p-2 text-xs">
                      {stderr}
                    </pre>
                  </div>
                ))}
            </div>
          </TableCell>
        </TableRow>
      )}
    </>
  );
}

export function CredentialStatusTable({ entries }: { entries: CredentialStatusEntry[] }) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Component</TableHead>
          <TableHead>Type</TableHead>
          <TableHead>Provider</TableHead>
          <TableHead>Context / Audience</TableHead>
          <TableHead>Expiry</TableHead>
          <TableHead>Connectivity</TableHead>
          <TableHead></TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {entries.map((entry) => (
          <CredentialStatusRow key={entry.component_id} entry={entry} />
        ))}
      </TableBody>
    </Table>
  );
}
