import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { cn } from '@/lib/utils';

interface MetricsCardProps {
  title: string;
  value: number | string;
  subLabel?: string;
  colorClass?: string;
}

export function MetricsCard({ title, value, subLabel, colorClass }: MetricsCardProps) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground">{title}</CardTitle>
      </CardHeader>
      <CardContent>
        <div className={cn('text-3xl font-bold', colorClass)}>{value}</div>
        {subLabel && <p className="mt-1 text-xs text-muted-foreground">{subLabel}</p>}
      </CardContent>
    </Card>
  );
}
