import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Calendar, Plus, Trash2, RefreshCw } from 'lucide-react';
import { getHolidays, createHoliday, deleteHoliday, addDefaultHolidays } from '../api/client';

const styles = {
  container: {
    padding: '24px',
    maxWidth: '900px',
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
  actions: {
    display: 'flex',
    gap: '12px',
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
  },
  primaryButton: {
    backgroundColor: '#3b82f6',
    color: 'white',
  },
  secondaryButton: {
    backgroundColor: '#f3f4f6',
    color: '#374151',
    border: '1px solid #e5e7eb',
  },
  dangerButton: {
    backgroundColor: '#fef2f2',
    color: '#dc2626',
    border: '1px solid #fecaca',
  },
  form: {
    backgroundColor: 'white',
    padding: '20px',
    borderRadius: '12px',
    border: '1px solid #e5e7eb',
    marginBottom: '24px',
  },
  formRow: {
    display: 'grid',
    gridTemplateColumns: '1fr 1fr 1fr auto auto',
    gap: '12px',
    alignItems: 'end',
  },
  formGroup: {
    display: 'flex',
    flexDirection: 'column' as const,
    gap: '4px',
  },
  label: {
    fontSize: '14px',
    fontWeight: 500,
    color: '#374151',
  },
  input: {
    padding: '10px 12px',
    border: '1px solid #e5e7eb',
    borderRadius: '8px',
    fontSize: '14px',
  },
  checkbox: {
    display: 'flex',
    alignItems: 'center',
    gap: '8px',
    padding: '10px 0',
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
    padding: '4px 8px',
    borderRadius: '9999px',
    fontSize: '12px',
    fontWeight: 500,
  },
  recurringBadge: {
    backgroundColor: '#dbeafe',
    color: '#1d4ed8',
  },
  oneTimeBadge: {
    backgroundColor: '#f3f4f6',
    color: '#6b7280',
  },
  emptyState: {
    textAlign: 'center' as const,
    padding: '48px',
    color: '#6b7280',
  },
};

export function HolidayAdmin() {
  const queryClient = useQueryClient();
  const [newHoliday, setNewHoliday] = useState({
    date: '',
    name: '',
    recurring: true,
    company_id: '',
  });

  const { data, isLoading } = useQuery({
    queryKey: ['holidays'],
    queryFn: () => getHolidays(),
  });

  const createMutation = useMutation({
    mutationFn: createHoliday,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['holidays'] });
      setNewHoliday({ date: '', name: '', recurring: true, company_id: '' });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: deleteHoliday,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['holidays'] });
    },
  });

  const addDefaultsMutation = useMutation({
    mutationFn: () => addDefaultHolidays(),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['holidays'] });
    },
  });

  const holidays = data?.holidays || [];

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (newHoliday.date && newHoliday.name) {
      createMutation.mutate(newHoliday);
    }
  };

  return (
    <div style={styles.container}>
      <div style={styles.header}>
        <h1 style={styles.title}>
          <Calendar size={28} />
          Holiday Calendar
        </h1>
        <div style={styles.actions}>
          <button
            style={{ ...styles.button, ...styles.secondaryButton }}
            onClick={() => addDefaultsMutation.mutate()}
            disabled={addDefaultsMutation.isPending}
          >
            <RefreshCw size={16} />
            Add Default Holidays
          </button>
        </div>
      </div>

      <form style={styles.form} onSubmit={handleSubmit}>
        <div style={styles.formRow}>
          <div style={styles.formGroup}>
            <label style={styles.label}>Date</label>
            <input
              type="date"
              style={styles.input}
              value={newHoliday.date}
              onChange={(e) => setNewHoliday({ ...newHoliday, date: e.target.value })}
              required
            />
          </div>
          <div style={styles.formGroup}>
            <label style={styles.label}>Holiday Name</label>
            <input
              type="text"
              style={styles.input}
              placeholder="e.g., Christmas Day"
              value={newHoliday.name}
              onChange={(e) => setNewHoliday({ ...newHoliday, name: e.target.value })}
              required
            />
          </div>
          <div style={styles.formGroup}>
            <label style={styles.checkbox}>
              <input
                type="checkbox"
                checked={newHoliday.recurring}
                onChange={(e) => setNewHoliday({ ...newHoliday, recurring: e.target.checked })}
              />
              <span>Recurring (every year)</span>
            </label>
          </div>
          <button
            type="submit"
            style={{ ...styles.button, ...styles.primaryButton }}
            disabled={createMutation.isPending}
          >
            <Plus size={16} />
            Add Holiday
          </button>
        </div>
      </form>

      {isLoading ? (
        <div style={styles.emptyState}>Loading holidays...</div>
      ) : holidays.length === 0 ? (
        <div style={styles.emptyState}>
          <Calendar size={48} style={{ marginBottom: '16px', opacity: 0.5 }} />
          <p>No holidays configured.</p>
          <p style={{ fontSize: '14px' }}>Add holidays above or click "Add Default Holidays" to get started.</p>
        </div>
      ) : (
        <table style={styles.table}>
          <thead>
            <tr>
              <th style={styles.th}>Date</th>
              <th style={styles.th}>Name</th>
              <th style={styles.th}>Type</th>
              <th style={styles.th}>Actions</th>
            </tr>
          </thead>
          <tbody>
            {holidays.map((holiday) => (
              <tr key={holiday.id}>
                <td style={styles.td}>
                  {new Date(holiday.date + 'T00:00:00').toLocaleDateString('en-US', {
                    weekday: 'short',
                    month: 'short',
                    day: 'numeric',
                  })}
                </td>
                <td style={styles.td}>{holiday.name}</td>
                <td style={styles.td}>
                  <span
                    style={{
                      ...styles.badge,
                      ...(holiday.recurring ? styles.recurringBadge : styles.oneTimeBadge),
                    }}
                  >
                    {holiday.recurring ? 'Recurring' : 'One-time'}
                  </span>
                </td>
                <td style={styles.td}>
                  <button
                    style={{ ...styles.button, ...styles.dangerButton, padding: '6px 12px' }}
                    onClick={() => deleteMutation.mutate(holiday.id)}
                    disabled={deleteMutation.isPending}
                  >
                    <Trash2 size={14} />
                    Delete
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
