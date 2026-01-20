import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { RefreshCw, CheckCircle, XCircle, Clock, ArrowRight, Calendar } from 'lucide-react';
import { getReconciliationRuns, triggerReconciliation } from '../api/client';

const styles = {
  container: {
    padding: '24px',
    maxWidth: '1000px',
  },
  header: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    marginBottom: '24px',
  },
  title: {
    display: 'flex',
    alignItems: 'center',
    gap: '12px',
    fontSize: '24px',
    fontWeight: 600,
    color: '#1f2937',
  },
  button: {
    display: 'flex',
    alignItems: 'center',
    gap: '8px',
    padding: '10px 16px',
    border: 'none',
    borderRadius: '8px',
    fontSize: '14px',
    fontWeight: 500,
    cursor: 'pointer',
    transition: 'all 0.2s',
    backgroundColor: '#3b82f6',
    color: 'white',
  },
  infoCard: {
    backgroundColor: '#f0fdf4',
    border: '1px solid #bbf7d0',
    borderRadius: '12px',
    padding: '20px',
    marginBottom: '24px',
  },
  infoTitle: {
    display: 'flex',
    alignItems: 'center',
    gap: '8px',
    fontSize: '16px',
    fontWeight: 600,
    color: '#166534',
    marginBottom: '8px',
  },
  infoText: {
    fontSize: '14px',
    color: '#15803d',
    lineHeight: 1.6,
  },
  section: {
    marginBottom: '32px',
  },
  sectionTitle: {
    fontSize: '18px',
    fontWeight: 600,
    color: '#1f2937',
    marginBottom: '16px',
    display: 'flex',
    alignItems: 'center',
    gap: '8px',
  },
  table: {
    width: '100%',
    borderCollapse: 'collapse' as const,
    backgroundColor: 'white',
    borderRadius: '12px',
    overflow: 'hidden',
    border: '1px solid #e5e7eb',
  },
  th: {
    padding: '12px 16px',
    textAlign: 'left' as const,
    fontSize: '12px',
    fontWeight: 600,
    color: '#6b7280',
    backgroundColor: '#f9fafb',
    textTransform: 'uppercase' as const,
    letterSpacing: '0.05em',
  },
  td: {
    padding: '12px 16px',
    borderTop: '1px solid #e5e7eb',
    fontSize: '14px',
    color: '#374151',
  },
  badge: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: '4px',
    padding: '4px 10px',
    borderRadius: '9999px',
    fontSize: '12px',
    fontWeight: 500,
  },
  completedBadge: {
    backgroundColor: '#dcfce7',
    color: '#166534',
  },
  pendingBadge: {
    backgroundColor: '#fef3c7',
    color: '#d97706',
  },
  runningBadge: {
    backgroundColor: '#dbeafe',
    color: '#1d4ed8',
  },
  failedBadge: {
    backgroundColor: '#fef2f2',
    color: '#dc2626',
  },
  resultCell: {
    display: 'flex',
    alignItems: 'center',
    gap: '16px',
  },
  resultItem: {
    display: 'flex',
    alignItems: 'center',
    gap: '4px',
    fontSize: '13px',
  },
  carried: {
    color: '#166534',
  },
  expired: {
    color: '#dc2626',
  },
  emptyState: {
    textAlign: 'center' as const,
    padding: '48px',
    color: '#6b7280',
    backgroundColor: 'white',
    borderRadius: '12px',
    border: '1px solid #e5e7eb',
  },
};

export function ReconciliationAdmin() {
  const queryClient = useQueryClient();

  const { data, isLoading } = useQuery({
    queryKey: ['reconciliationRuns'],
    queryFn: () => getReconciliationRuns(),
    refetchInterval: 60000, // Refresh every minute
  });

  const triggerMutation = useMutation({
    mutationFn: () => {
      const today = new Date();
      const lastYearEnd = new Date(today.getFullYear() - 1, 11, 31);
      return triggerReconciliation({
        period_end: lastYearEnd.toISOString().split('T')[0],
      });
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['reconciliationRuns'] });
      queryClient.invalidateQueries({ queryKey: ['transactions'] });
      queryClient.invalidateQueries({ queryKey: ['balance'] });
    },
  });

  const runs = data?.runs || [];
  const completedRuns = runs.filter((r) => r.status === 'completed');
  const pendingRuns = runs.filter((r) => r.status === 'pending' || r.status === 'running');

  const getStatusBadge = (status: string) => {
    switch (status) {
      case 'completed':
        return { ...styles.badge, ...styles.completedBadge };
      case 'pending':
        return { ...styles.badge, ...styles.pendingBadge };
      case 'running':
        return { ...styles.badge, ...styles.runningBadge };
      case 'failed':
        return { ...styles.badge, ...styles.failedBadge };
      default:
        return styles.badge;
    }
  };

  const getStatusIcon = (status: string) => {
    switch (status) {
      case 'completed':
        return <CheckCircle size={14} />;
      case 'pending':
        return <Clock size={14} />;
      case 'running':
        return <RefreshCw size={14} className="animate-spin" />;
      case 'failed':
        return <XCircle size={14} />;
      default:
        return null;
    }
  };

  return (
    <div style={styles.container}>
      <div style={styles.header}>
        <h1 style={styles.title}>
          <RefreshCw size={28} />
          Reconciliation
        </h1>
        <button
          style={styles.button}
          onClick={() => triggerMutation.mutate()}
          disabled={triggerMutation.isPending}
        >
          <RefreshCw size={16} />
          Trigger Year-End Reconciliation
        </button>
      </div>

      <div style={styles.infoCard}>
        <div style={styles.infoTitle}>
          <Calendar size={18} />
          Automated Reconciliation
        </div>
        <p style={styles.infoText}>
          The system automatically checks for policy periods that have ended and processes
          reconciliation (carryover and expiration) based on each policy's rules. This runs
          hourly in the background. You can also trigger it manually using the button above.
        </p>
      </div>

      {pendingRuns.length > 0 && (
        <div style={styles.section}>
          <h2 style={styles.sectionTitle}>
            <Clock size={20} />
            In Progress
          </h2>
          <table style={styles.table}>
            <thead>
              <tr>
                <th style={styles.th}>Policy</th>
                <th style={styles.th}>Employee</th>
                <th style={styles.th}>Period</th>
                <th style={styles.th}>Status</th>
              </tr>
            </thead>
            <tbody>
              {pendingRuns.map((run) => (
                <tr key={run.id}>
                  <td style={styles.td}>{run.policy_id}</td>
                  <td style={styles.td}>{run.entity_id}</td>
                  <td style={styles.td}>
                    {new Date(run.period_start).toLocaleDateString()} –{' '}
                    {new Date(run.period_end).toLocaleDateString()}
                  </td>
                  <td style={styles.td}>
                    <span style={getStatusBadge(run.status)}>
                      {getStatusIcon(run.status)}
                      {run.status.charAt(0).toUpperCase() + run.status.slice(1)}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <div style={styles.section}>
        <h2 style={styles.sectionTitle}>
          <CheckCircle size={20} />
          Reconciliation History
        </h2>

        {isLoading ? (
          <div style={styles.emptyState}>Loading reconciliation history...</div>
        ) : completedRuns.length === 0 ? (
          <div style={styles.emptyState}>
            <RefreshCw size={48} style={{ marginBottom: '16px', opacity: 0.5 }} />
            <p>No reconciliations have been processed yet.</p>
            <p style={{ fontSize: '14px', marginTop: '8px' }}>
              Reconciliation runs when a policy period ends (e.g., year-end).
            </p>
          </div>
        ) : (
          <table style={styles.table}>
            <thead>
              <tr>
                <th style={styles.th}>Policy</th>
                <th style={styles.th}>Employee</th>
                <th style={styles.th}>Period</th>
                <th style={styles.th}>Result</th>
                <th style={styles.th}>Completed</th>
                <th style={styles.th}>Status</th>
              </tr>
            </thead>
            <tbody>
              {completedRuns.map((run) => (
                <tr key={run.id}>
                  <td style={styles.td}>{run.policy_id}</td>
                  <td style={styles.td}>{run.entity_id}</td>
                  <td style={styles.td}>
                    {new Date(run.period_start).toLocaleDateString()} –{' '}
                    {new Date(run.period_end).toLocaleDateString()}
                  </td>
                  <td style={styles.td}>
                    <div style={styles.resultCell}>
                      <span style={{ ...styles.resultItem, ...styles.carried }}>
                        <ArrowRight size={14} />
                        {run.carried_over.toFixed(1)} carried
                      </span>
                      <span style={{ ...styles.resultItem, ...styles.expired }}>
                        <XCircle size={14} />
                        {run.expired.toFixed(1)} expired
                      </span>
                    </div>
                  </td>
                  <td style={styles.td}>
                    {run.completed_at
                      ? new Date(run.completed_at).toLocaleString()
                      : '-'}
                  </td>
                  <td style={styles.td}>
                    <span style={getStatusBadge(run.status)}>
                      {getStatusIcon(run.status)}
                      {run.status.charAt(0).toUpperCase() + run.status.slice(1)}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
