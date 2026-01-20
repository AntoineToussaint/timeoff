import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { CheckCircle, XCircle, Clock, User, Calendar } from 'lucide-react';
import { getPendingRequests, approveRequest, rejectRequest } from '../api/client';

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
  badge: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    minWidth: '24px',
    height: '24px',
    padding: '0 8px',
    borderRadius: '9999px',
    fontSize: '14px',
    fontWeight: 600,
    backgroundColor: '#fef3c7',
    color: '#d97706',
  },
  card: {
    backgroundColor: 'white',
    borderRadius: '12px',
    border: '1px solid #e5e7eb',
    marginBottom: '16px',
    overflow: 'hidden',
  },
  cardHeader: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '16px 20px',
    borderBottom: '1px solid #f3f4f6',
    backgroundColor: '#f9fafb',
  },
  employeeInfo: {
    display: 'flex',
    alignItems: 'center',
    gap: '12px',
  },
  avatar: {
    width: '40px',
    height: '40px',
    borderRadius: '9999px',
    backgroundColor: '#e0e7ff',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    color: '#4f46e5',
  },
  employeeName: {
    fontSize: '16px',
    fontWeight: 600,
    color: '#1f2937',
  },
  requestType: {
    fontSize: '14px',
    color: '#6b7280',
    textTransform: 'capitalize' as const,
  },
  cardBody: {
    padding: '20px',
  },
  detailsGrid: {
    display: 'grid',
    gridTemplateColumns: 'repeat(3, 1fr)',
    gap: '16px',
    marginBottom: '16px',
  },
  detailItem: {
    display: 'flex',
    flexDirection: 'column' as const,
    gap: '4px',
  },
  detailLabel: {
    fontSize: '12px',
    fontWeight: 500,
    color: '#6b7280',
    textTransform: 'uppercase' as const,
    letterSpacing: '0.05em',
  },
  detailValue: {
    fontSize: '16px',
    fontWeight: 500,
    color: '#1f2937',
  },
  reason: {
    padding: '12px 16px',
    backgroundColor: '#f9fafb',
    borderRadius: '8px',
    fontSize: '14px',
    color: '#4b5563',
    marginBottom: '16px',
  },
  actions: {
    display: 'flex',
    gap: '12px',
    justifyContent: 'flex-end',
  },
  button: {
    display: 'flex',
    alignItems: 'center',
    gap: '8px',
    padding: '10px 20px',
    border: 'none',
    borderRadius: '8px',
    fontSize: '14px',
    fontWeight: 500,
    cursor: 'pointer',
    transition: 'all 0.2s',
  },
  approveButton: {
    backgroundColor: '#10b981',
    color: 'white',
  },
  rejectButton: {
    backgroundColor: '#fef2f2',
    color: '#dc2626',
    border: '1px solid #fecaca',
  },
  emptyState: {
    textAlign: 'center' as const,
    padding: '64px',
    color: '#6b7280',
    backgroundColor: 'white',
    borderRadius: '12px',
    border: '1px solid #e5e7eb',
  },
  modal: {
    position: 'fixed' as const,
    inset: 0,
    backgroundColor: 'rgba(0, 0, 0, 0.5)',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    zIndex: 50,
  },
  modalContent: {
    backgroundColor: 'white',
    padding: '24px',
    borderRadius: '12px',
    width: '400px',
    maxWidth: '90vw',
  },
  modalTitle: {
    fontSize: '18px',
    fontWeight: 600,
    marginBottom: '16px',
    color: '#1f2937',
  },
  textarea: {
    width: '100%',
    padding: '12px',
    border: '1px solid #e5e7eb',
    borderRadius: '8px',
    fontSize: '14px',
    minHeight: '100px',
    resize: 'vertical' as const,
    marginBottom: '16px',
  },
  modalActions: {
    display: 'flex',
    gap: '12px',
    justifyContent: 'flex-end',
  },
  cancelButton: {
    backgroundColor: '#f3f4f6',
    color: '#374151',
    border: '1px solid #e5e7eb',
  },
};

export function ApprovalQueue() {
  const queryClient = useQueryClient();
  const [rejectModalId, setRejectModalId] = useState<string | null>(null);
  const [rejectReason, setRejectReason] = useState('');

  const { data, isLoading } = useQuery({
    queryKey: ['pendingRequests'],
    queryFn: getPendingRequests,
    refetchInterval: 30000, // Refresh every 30 seconds
  });

  const approveMutation = useMutation({
    mutationFn: (id: string) => approveRequest(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['pendingRequests'] });
      queryClient.invalidateQueries({ queryKey: ['transactions'] });
      queryClient.invalidateQueries({ queryKey: ['balance'] });
    },
  });

  const rejectMutation = useMutation({
    mutationFn: ({ id, reason }: { id: string; reason: string }) => rejectRequest(id, reason),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['pendingRequests'] });
      queryClient.invalidateQueries({ queryKey: ['transactions'] });
      queryClient.invalidateQueries({ queryKey: ['balance'] });
      setRejectModalId(null);
      setRejectReason('');
    },
  });

  const requests = data?.requests || [];

  const handleReject = () => {
    if (rejectModalId && rejectReason.trim()) {
      rejectMutation.mutate({ id: rejectModalId, reason: rejectReason });
    }
  };

  const formatResourceType = (type: string) => {
    return type.replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase());
  };

  return (
    <div style={styles.container}>
      <div style={styles.header}>
        <h1 style={styles.title}>
          <Clock size={28} />
          Approval Queue
          {requests.length > 0 && <span style={styles.badge}>{requests.length}</span>}
        </h1>
      </div>

      {isLoading ? (
        <div style={styles.emptyState}>Loading pending requests...</div>
      ) : requests.length === 0 ? (
        <div style={styles.emptyState}>
          <CheckCircle size={48} style={{ marginBottom: '16px', color: '#10b981' }} />
          <p style={{ fontSize: '18px', fontWeight: 500, marginBottom: '8px' }}>All caught up!</p>
          <p style={{ fontSize: '14px' }}>No pending requests require approval.</p>
        </div>
      ) : (
        requests.map((request) => (
          <div key={request.id} style={styles.card}>
            <div style={styles.cardHeader}>
              <div style={styles.employeeInfo}>
                <div style={styles.avatar}>
                  <User size={20} />
                </div>
                <div>
                  <div style={styles.employeeName}>{request.employee_name}</div>
                  <div style={styles.requestType}>{formatResourceType(request.resource_type)}</div>
                </div>
              </div>
              <div style={{ display: 'flex', alignItems: 'center', gap: '8px', color: '#6b7280' }}>
                <Calendar size={16} />
                {new Date(request.created_at).toLocaleDateString()}
              </div>
            </div>
            <div style={styles.cardBody}>
              <div style={styles.detailsGrid}>
                <div style={styles.detailItem}>
                  <span style={styles.detailLabel}>Date</span>
                  <span style={styles.detailValue}>
                    {new Date(request.effective_at + 'T00:00:00').toLocaleDateString('en-US', {
                      weekday: 'short',
                      month: 'short',
                      day: 'numeric',
                      year: 'numeric',
                    })}
                  </span>
                </div>
                <div style={styles.detailItem}>
                  <span style={styles.detailLabel}>Amount</span>
                  <span style={styles.detailValue}>
                    {request.amount} {request.unit}
                  </span>
                </div>
                <div style={styles.detailItem}>
                  <span style={styles.detailLabel}>Submitted</span>
                  <span style={styles.detailValue}>
                    {new Date(request.created_at).toLocaleTimeString('en-US', {
                      hour: 'numeric',
                      minute: '2-digit',
                    })}
                  </span>
                </div>
              </div>

              {request.reason && (
                <div style={styles.reason}>
                  <strong>Reason:</strong> {request.reason}
                </div>
              )}

              <div style={styles.actions}>
                <button
                  style={{ ...styles.button, ...styles.rejectButton }}
                  onClick={() => setRejectModalId(request.id)}
                  disabled={rejectMutation.isPending}
                >
                  <XCircle size={16} />
                  Reject
                </button>
                <button
                  style={{ ...styles.button, ...styles.approveButton }}
                  onClick={() => approveMutation.mutate(request.id)}
                  disabled={approveMutation.isPending}
                >
                  <CheckCircle size={16} />
                  Approve
                </button>
              </div>
            </div>
          </div>
        ))
      )}

      {/* Reject Modal */}
      {rejectModalId && (
        <div style={styles.modal} onClick={() => setRejectModalId(null)}>
          <div style={styles.modalContent} onClick={(e) => e.stopPropagation()}>
            <h3 style={styles.modalTitle}>Reject Request</h3>
            <textarea
              style={styles.textarea}
              placeholder="Please provide a reason for rejection..."
              value={rejectReason}
              onChange={(e) => setRejectReason(e.target.value)}
            />
            <div style={styles.modalActions}>
              <button
                style={{ ...styles.button, ...styles.cancelButton }}
                onClick={() => {
                  setRejectModalId(null);
                  setRejectReason('');
                }}
              >
                Cancel
              </button>
              <button
                style={{ ...styles.button, ...styles.rejectButton }}
                onClick={handleReject}
                disabled={!rejectReason.trim() || rejectMutation.isPending}
              >
                <XCircle size={16} />
                Reject Request
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
