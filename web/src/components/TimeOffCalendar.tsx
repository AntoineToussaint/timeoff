import { useState, useMemo } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Calendar, ChevronLeft, ChevronRight, AlertCircle } from 'lucide-react';
import { cancelTransaction } from '../api/client';
import type { Transaction } from '../api/client';
import { format, startOfMonth, endOfMonth, eachDayOfInterval, isSameMonth, isSameDay, addMonths, subMonths, parseISO } from 'date-fns';

const styles = {
  container: {
    background: 'var(--bg-secondary)',
    borderRadius: '12px',
    padding: '1.5rem',
    marginTop: '1.5rem',
  },
  header: {
    display: 'flex',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: '1rem',
  },
  title: {
    display: 'flex',
    alignItems: 'center',
    gap: '0.5rem',
    margin: 0,
    fontSize: '1.1rem',
    fontWeight: 600,
  },
  navButton: {
    background: 'var(--bg-tertiary)',
    border: 'none',
    borderRadius: '6px',
    padding: '0.5rem',
    cursor: 'pointer',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    color: 'var(--text-primary)',
  },
  monthYear: {
    fontSize: '1rem',
    fontWeight: 500,
  },
  weekDays: {
    display: 'grid',
    gridTemplateColumns: 'repeat(7, 1fr)',
    gap: '4px',
    marginBottom: '0.5rem',
  },
  weekDay: {
    textAlign: 'center' as const,
    fontSize: '0.75rem',
    fontWeight: 500,
    color: 'var(--text-muted)',
    padding: '0.5rem',
  },
  days: {
    display: 'grid',
    gridTemplateColumns: 'repeat(7, 1fr)',
    gap: '4px',
  },
  day: {
    aspectRatio: '1',
    display: 'flex',
    flexDirection: 'column' as const,
    alignItems: 'center',
    justifyContent: 'center',
    borderRadius: '8px',
    fontSize: '0.9rem',
    cursor: 'default',
    position: 'relative' as const,
    minHeight: '40px',
  },
  dayOtherMonth: {
    color: 'var(--text-muted)',
    opacity: 0.3,
  },
  dayToday: {
    border: '2px solid var(--accent)',
  },
  dayConsumption: {
    background: 'var(--accent)',
    color: 'white',
    cursor: 'pointer',
  },
  dayPending: {
    background: 'var(--warning)',
    color: 'white',
    cursor: 'pointer',
  },
  dayCancelled: {
    background: 'var(--bg-tertiary)',
    color: 'var(--text-muted)',
    textDecoration: 'line-through',
  },
  dayNumber: {
    fontWeight: 500,
  },
  dayType: {
    fontSize: '0.6rem',
    opacity: 0.8,
    textTransform: 'uppercase' as const,
  },
  legend: {
    display: 'flex',
    gap: '1rem',
    marginTop: '1rem',
    flexWrap: 'wrap' as const,
  },
  legendItem: {
    display: 'flex',
    alignItems: 'center',
    gap: '0.5rem',
    fontSize: '0.8rem',
    color: 'var(--text-secondary)',
  },
  legendDot: {
    width: '12px',
    height: '12px',
    borderRadius: '3px',
  },
  modal: {
    position: 'fixed' as const,
    top: 0,
    left: 0,
    right: 0,
    bottom: 0,
    background: 'rgba(0,0,0,0.5)',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    zIndex: 1000,
  },
  modalContent: {
    background: 'var(--bg-primary)',
    borderRadius: '12px',
    padding: '1.5rem',
    maxWidth: '400px',
    width: '90%',
  },
  modalTitle: {
    margin: '0 0 1rem 0',
    fontSize: '1.1rem',
  },
  modalText: {
    color: 'var(--text-secondary)',
    marginBottom: '1.5rem',
  },
  modalButtons: {
    display: 'flex',
    gap: '0.75rem',
    justifyContent: 'flex-end',
  },
  cancelBtn: {
    background: 'var(--bg-tertiary)',
    border: 'none',
    borderRadius: '8px',
    padding: '0.75rem 1.25rem',
    cursor: 'pointer',
    color: 'var(--text-primary)',
  },
  confirmBtn: {
    background: 'var(--danger)',
    border: 'none',
    borderRadius: '8px',
    padding: '0.75rem 1.25rem',
    cursor: 'pointer',
    color: 'white',
    fontWeight: 500,
  },
  error: {
    background: 'rgba(239, 68, 68, 0.1)',
    border: '1px solid var(--danger)',
    borderRadius: '8px',
    padding: '1rem',
    display: 'flex',
    alignItems: 'center',
    gap: '0.5rem',
    color: 'var(--danger)',
    marginBottom: '1rem',
  },
};

interface TimeOffCalendarProps {
  transactions: Transaction[];
  employeeId: string;
  resourceType: string;
}

interface DayInfo {
  date: Date;
  transactions: Transaction[];
  status: 'none' | 'consumption' | 'pending' | 'cancelled';
}

export function TimeOffCalendar({ transactions, employeeId, resourceType }: TimeOffCalendarProps) {
  const [currentMonth, setCurrentMonth] = useState(new Date());
  const [selectedDay, setSelectedDay] = useState<DayInfo | null>(null);
  const [error, setError] = useState<string | null>(null);
  const queryClient = useQueryClient();

  const cancelMutation = useMutation({
    mutationFn: (txId: string) => cancelTransaction(txId),
    onSuccess: () => {
      // Invalidate queries to refresh data
      queryClient.invalidateQueries({ queryKey: ['transactions', employeeId] });
      queryClient.invalidateQueries({ queryKey: ['balance', employeeId, resourceType] });
      setSelectedDay(null);
      setError(null);
    },
    onError: (err: Error) => {
      setError(err.message);
    },
  });

  // Build a map of days with time off
  const dayMap = useMemo(() => {
    const map = new Map<string, DayInfo>();
    const reversedTxIds = new Set<string>();

    // First pass: find all reversed transactions
    transactions.forEach(tx => {
      if (tx.type === 'reversal' && tx.reference_id) {
        reversedTxIds.add(tx.reference_id);
      }
    });

    // Second pass: build day info
    transactions.forEach(tx => {
      if (tx.resource_type !== resourceType) return;
      if (tx.type !== 'consumption' && tx.type !== 'pending') return;

      const dateKey = format(parseISO(tx.effective_at), 'yyyy-MM-dd');
      const date = parseISO(tx.effective_at);
      const isCancelled = reversedTxIds.has(tx.id);

      const existing = map.get(dateKey);
      if (existing) {
        existing.transactions.push(tx);
        // Update status (cancelled takes precedence for display)
        if (isCancelled) {
          existing.status = 'cancelled';
        }
      } else {
        map.set(dateKey, {
          date,
          transactions: [tx],
          status: isCancelled ? 'cancelled' : (tx.type as 'consumption' | 'pending'),
        });
      }
    });

    return map;
  }, [transactions, resourceType]);

  // Get days for the calendar
  const monthStart = startOfMonth(currentMonth);
  const monthEnd = endOfMonth(currentMonth);
  
  // Get the first day of the week for the month
  const calendarStart = new Date(monthStart);
  calendarStart.setDate(calendarStart.getDate() - calendarStart.getDay());
  
  // Get the last day of the week for the month
  const calendarEnd = new Date(monthEnd);
  calendarEnd.setDate(calendarEnd.getDate() + (6 - calendarEnd.getDay()));

  const days = eachDayOfInterval({ start: calendarStart, end: calendarEnd });
  const today = new Date();

  const handleDayClick = (dayInfo: DayInfo) => {
    if (dayInfo.status === 'consumption' || dayInfo.status === 'pending') {
      setSelectedDay(dayInfo);
      setError(null);
    }
  };

  const handleCancel = () => {
    if (!selectedDay) return;
    // Cancel the first non-reversed transaction for this day
    const txToCancel = selectedDay.transactions.find(tx => 
      tx.type === 'consumption' || tx.type === 'pending'
    );
    if (txToCancel) {
      cancelMutation.mutate(txToCancel.id);
    }
  };

  return (
    <div style={styles.container}>
      <div style={styles.header}>
        <h3 style={styles.title}>
          <Calendar size={18} />
          Time Off Calendar
        </h3>
        <div style={{ display: 'flex', alignItems: 'center', gap: '1rem' }}>
          <button
            style={styles.navButton}
            onClick={() => setCurrentMonth(subMonths(currentMonth, 1))}
          >
            <ChevronLeft size={18} />
          </button>
          <span style={styles.monthYear}>
            {format(currentMonth, 'MMMM yyyy')}
          </span>
          <button
            style={styles.navButton}
            onClick={() => setCurrentMonth(addMonths(currentMonth, 1))}
          >
            <ChevronRight size={18} />
          </button>
        </div>
      </div>

      <div style={styles.weekDays}>
        {['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'].map(day => (
          <div key={day} style={styles.weekDay}>{day}</div>
        ))}
      </div>

      <div style={styles.days}>
        {days.map(day => {
          const dateKey = format(day, 'yyyy-MM-dd');
          const dayInfo = dayMap.get(dateKey);
          const isCurrentMonth = isSameMonth(day, currentMonth);
          const isToday = isSameDay(day, today);

          let dayStyle = { ...styles.day };
          if (!isCurrentMonth) {
            dayStyle = { ...dayStyle, ...styles.dayOtherMonth };
          }
          if (isToday) {
            dayStyle = { ...dayStyle, ...styles.dayToday };
          }
          if (dayInfo) {
            if (dayInfo.status === 'consumption') {
              dayStyle = { ...dayStyle, ...styles.dayConsumption };
            } else if (dayInfo.status === 'pending') {
              dayStyle = { ...dayStyle, ...styles.dayPending };
            } else if (dayInfo.status === 'cancelled') {
              dayStyle = { ...dayStyle, ...styles.dayCancelled };
            }
          }

          return (
            <div
              key={dateKey}
              style={dayStyle}
              onClick={() => dayInfo && handleDayClick(dayInfo)}
              title={dayInfo ? `Click to cancel` : undefined}
            >
              <span style={styles.dayNumber}>{format(day, 'd')}</span>
              {dayInfo && dayInfo.status !== 'cancelled' && (
                <span style={styles.dayType}>
                  {dayInfo.status === 'pending' ? 'pending' : 'off'}
                </span>
              )}
              {dayInfo?.status === 'cancelled' && (
                <span style={styles.dayType}>cancelled</span>
              )}
            </div>
          );
        })}
      </div>

      <div style={styles.legend}>
        <div style={styles.legendItem}>
          <div style={{ ...styles.legendDot, background: 'var(--accent)' }} />
          <span>Approved</span>
        </div>
        <div style={styles.legendItem}>
          <div style={{ ...styles.legendDot, background: 'var(--warning)' }} />
          <span>Pending</span>
        </div>
        <div style={styles.legendItem}>
          <div style={{ ...styles.legendDot, background: 'var(--bg-tertiary)' }} />
          <span>Cancelled</span>
        </div>
      </div>

      {/* Cancel confirmation modal */}
      {selectedDay && (
        <div style={styles.modal} onClick={() => setSelectedDay(null)}>
          <div style={styles.modalContent} onClick={e => e.stopPropagation()}>
            <h3 style={styles.modalTitle}>
              Cancel Time Off - {format(selectedDay.date, 'MMMM d, yyyy')}
            </h3>
            {error && (
              <div style={styles.error}>
                <AlertCircle size={18} />
                {error}
              </div>
            )}
            <p style={styles.modalText}>
              Are you sure you want to cancel this day off? 
              Your balance will be restored.
            </p>
            <div style={styles.modalButtons}>
              <button
                style={styles.cancelBtn}
                onClick={() => setSelectedDay(null)}
              >
                Keep It
              </button>
              <button
                style={styles.confirmBtn}
                onClick={handleCancel}
                disabled={cancelMutation.isPending}
              >
                {cancelMutation.isPending ? 'Cancelling...' : 'Cancel Day Off'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
