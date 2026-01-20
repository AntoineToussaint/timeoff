import { useState, useMemo, useEffect } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useParams } from 'react-router-dom';
import { Calendar, Clock, AlertCircle, Check, User, Info } from 'lucide-react';
import { getEmployees, getBalance, getTransactions, submitRequest, getCurrentScenario, getAssignments, getPolicies } from '../api/client';
import type { Balance, Transaction } from '../api/client';
import { format, parseISO } from 'date-fns';
import { DateRangePicker } from './DateRangePicker';
import { TimeOffCalendar } from './TimeOffCalendar';

const RESOURCE_TYPES = [
  { id: 'pto', label: 'PTO', color: 'var(--accent)' },
  { id: 'sick', label: 'Sick', color: 'var(--warning)' },
  { id: 'parental', label: 'Parental', color: 'var(--success)' },
  { id: 'floating_holiday', label: 'Floating', color: 'var(--info)' },
  { id: 'bereavement', label: 'Bereavement', color: 'var(--text-muted)' },
];

export function EmployeeDashboard() {
  const { id } = useParams();
  const queryClient = useQueryClient();
  const [selectedEmployee, setSelectedEmployee] = useState<string | null>(id || null);
  const [selectedResourceType, setSelectedResourceType] = useState('pto');
  const [showRequestModal, setShowRequestModal] = useState(false);
  const [showDebug, setShowDebug] = useState(false);

  const { data: employees, isLoading: loadingEmployees } = useQuery({
    queryKey: ['employees'],
    queryFn: getEmployees,
    staleTime: 10000,
    refetchOnWindowFocus: false,
  });

  const { data: balance, isLoading: loadingBalance } = useQuery({
    queryKey: ['balance', selectedEmployee, selectedResourceType],
    queryFn: () => getBalance(selectedEmployee!, selectedResourceType),
    enabled: !!selectedEmployee && !!selectedResourceType,
    staleTime: 10000,
    refetchOnWindowFocus: false,
  });

  const { data: transactions } = useQuery({
    queryKey: ['transactions', selectedEmployee],
    queryFn: () => getTransactions(selectedEmployee!),
    enabled: !!selectedEmployee,
    staleTime: 10000,
    refetchOnWindowFocus: false,
  });

  const { data: currentScenario } = useQuery({
    queryKey: ['currentScenario'],
    queryFn: getCurrentScenario,
    staleTime: 10000,
    refetchOnWindowFocus: false,
  });

  // Query assignments and policies to determine available resource types
  const { data: assignments } = useQuery({
    queryKey: ['assignments', selectedEmployee],
    queryFn: () => getAssignments(selectedEmployee!),
    enabled: !!selectedEmployee,
    staleTime: 30000,
    refetchOnWindowFocus: false,
  });

  const { data: policies } = useQuery({
    queryKey: ['policies'],
    queryFn: getPolicies,
    staleTime: 30000,
    refetchOnWindowFocus: false,
  });

  // Filter resource types based on employee's assignments
  // An employee has access to a resource type if they have an assignment to a policy of that type
  const availableResourceTypes = useMemo(() => {
    // First try to determine from assignments + policies
    if (assignments && assignments.length > 0 && policies && policies.length > 0) {
      const resourceTypeSet = new Set<string>();
      for (const assign of assignments) {
        const policy = policies.find(p => p.id === assign.policy_id);
        // Check both top-level resource_type and config.resource_type
        const rt = policy?.resource_type || policy?.config?.resource_type;
        if (rt) {
          resourceTypeSet.add(rt);
        }
      }
      if (resourceTypeSet.size > 0) {
        return RESOURCE_TYPES.filter(rt => resourceTypeSet.has(rt.id));
      }
      // Has assignments but no matching resource types - default to PTO
      return RESOURCE_TYPES.filter(rt => rt.id === 'pto');
    }
    
    // Fallback: check transactions (for backwards compatibility)
    if (transactions && transactions.length > 0) {
      const resourceTypeSet = new Set(transactions.map(tx => tx.resource_type));
      return RESOURCE_TYPES.filter(rt => resourceTypeSet.has(rt.id));
    }
    
    // Has assignments but policies not loaded yet - default to PTO
    if (assignments && assignments.length > 0) {
      return RESOURCE_TYPES.filter(rt => rt.id === 'pto');
    }
    
    return [];
  }, [assignments, policies, transactions]);

  // Handle URL parameter and set default employee when employees load
  useEffect(() => {
    if (!employees || employees.length === 0) return;
    
    // If URL has employee ID and it exists, use it
    if (id && employees.some(e => e.id === id)) {
      setSelectedEmployee(id);
      return;
    }
    
    // If current selection exists in employees list, keep it
    if (selectedEmployee && employees.some(e => e.id === selectedEmployee)) {
      return;
    }
    
    // Otherwise select first employee
    setSelectedEmployee(employees[0].id);
  }, [id, employees, selectedEmployee]);

  // Set selected resource type when employee changes or types become available
  useEffect(() => {
    if (availableResourceTypes.length > 0) {
      // If current selection is not in available types, switch to first available
      if (!availableResourceTypes.some(rt => rt.id === selectedResourceType)) {
        setSelectedResourceType(availableResourceTypes[0].id);
      }
    }
    // Note: Don't reset to empty - keep 'pto' as default so balance query runs
  }, [availableResourceTypes, selectedResourceType]);

  // Check if current scenario is rewards and redirect
  useEffect(() => {
    if (currentScenario?.category === 'rewards') {
      // Don't redirect automatically, but we could show a message
      // Or redirect to rewards dashboard
    }
  }, [currentScenario]);

  if (loadingEmployees) {
    return <div className="loading"><div className="spinner" /> Loading...</div>;
  }

  if (!employees?.length) {
    return (
      <div className="empty-state">
        <User className="empty-state-icon" size={48} />
        <h2>No Employees</h2>
        <p>Load a scenario to get started</p>
      </div>
    );
  }

  const employee = employees.find(e => e.id === selectedEmployee) || employees[0];

  // Scenario details configuration (matching ScenarioLoader)
  const scenarioDetails: Record<string, { color: string }> = {
    'new-employee': { color: '#10b981' },
    'mid-year-hire': { color: '#3b82f6' },
    'year-end-rollover': { color: '#f59e0b' },
    'multi-policy': { color: '#8b5cf6' },
    'new-parent': { color: '#ec4899' },
    'rewards-benefits': { color: '#06b6d4' },
    'hourly-worker': { color: '#64748b' },
  };

  return (
    <div>
      <header style={{ marginBottom: '2rem' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '1rem', marginBottom: '0.5rem' }}>
          <h1 style={{ fontSize: '1.5rem', fontWeight: 600 }}>Employee Dashboard</h1>
          <select
            className="form-select"
            style={{ width: 'auto' }}
            value={selectedEmployee || ''}
            onChange={(e) => setSelectedEmployee(e.target.value)}
          >
            {employees.map(emp => (
              <option key={emp.id} value={emp.id}>{emp.name}</option>
            ))}
          </select>
        </div>
        <p style={{ color: 'var(--text-muted)', fontSize: '0.875rem' }}>
          {employee?.email} · Hired {employee?.hire_date}
        </p>
      </header>

      {/* Scenario Summary */}
      {currentScenario && (
        <div className="card" style={{ 
          marginBottom: '1.5rem', 
          background: `${scenarioDetails[currentScenario.id]?.color || 'var(--accent)'}10`,
          borderLeft: `3px solid ${scenarioDetails[currentScenario.id]?.color || 'var(--accent)'}`,
        }}>
          <div style={{ display: 'flex', alignItems: 'flex-start', gap: '0.75rem' }}>
            <Info size={20} style={{ 
              color: scenarioDetails[currentScenario.id]?.color || 'var(--accent)',
              marginTop: '2px',
              flexShrink: 0,
            }} />
            <div style={{ flex: 1 }}>
              <div style={{ fontSize: '0.875rem', fontWeight: 600, marginBottom: '0.25rem' }}>
                Active Scenario: {currentScenario.name}
              </div>
              <div style={{ fontSize: '0.8125rem', color: 'var(--text-muted)' }}>
                {currentScenario.description}
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Resource Type Tabs - Only show types employee actually has */}
      {availableResourceTypes.length > 0 && (
        <div className="tabs" style={{ marginBottom: '1.5rem' }}>
          {availableResourceTypes.map(rt => (
            <button
              key={rt.id}
              className={`tab ${selectedResourceType === rt.id ? 'active' : ''}`}
              onClick={() => setSelectedResourceType(rt.id)}
              style={{ 
                borderBottomColor: selectedResourceType === rt.id ? rt.color : 'transparent',
              }}
            >
              {rt.label}
            </button>
          ))}
        </div>
      )}

      {/* Show message if no tabs available */}
      {availableResourceTypes.length === 0 && !loadingBalance && (
        <div className="card" style={{ marginBottom: '1.5rem', padding: '1rem', background: 'rgba(239, 68, 68, 0.1)', borderColor: 'var(--danger)' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', color: 'var(--danger)' }}>
            <AlertCircle size={20} />
            <span>
              {currentScenario?.category === 'rewards' 
                ? 'This is a rewards scenario. Go to the Rewards dashboard to view rewards data.'
                : 'No time-off policies found for this employee.'}
            </span>
          </div>
        </div>
      )}

      {!selectedResourceType ? (
        <div className="empty-state">
          <User className="empty-state-icon" size={48} />
          <h2>No Time-Off Data</h2>
          <p>This employee doesn't have any time-off policies assigned.</p>
        </div>
      ) : loadingBalance ? (
        <div className="loading"><div className="spinner" /> Loading balance...</div>
      ) : balance && balance.policies && balance.policies.length > 0 ? (
        <>
          {/* Balance Overview */}
          <div className="card" style={{ marginBottom: '1.5rem' }}>
            <div className="card-header">
              <h2 className="card-title">
                {RESOURCE_TYPES.find(rt => rt.id === selectedResourceType)?.label || 'Balance'}
              </h2>
              <button className="btn btn-primary" onClick={() => setShowRequestModal(true)}>
                <Calendar size={16} />
                Request Time Off
              </button>
            </div>

            {/* Total Available Summary */}
            <div style={{ 
              display: 'flex', 
              alignItems: 'center', 
              gap: '1.5rem', 
              marginBottom: '1.5rem',
              padding: '1rem 1.5rem',
              background: 'var(--bg-dark)',
              borderRadius: '10px',
            }}>
              <div>
                <div style={{ fontSize: '0.75rem', color: 'var(--text-muted)', marginBottom: '0.25rem' }}>
                  Total Available
                </div>
                <div style={{ fontSize: '2rem', fontWeight: 700 }}>
                  {balance.total_available.toFixed(1)}
                  <span style={{ fontSize: '1rem', fontWeight: 400, color: 'var(--text-muted)', marginLeft: '0.25rem' }}>days</span>
                </div>
              </div>
              <div style={{ height: '40px', width: '1px', background: 'var(--border)' }} />
              <div style={{ flex: 1 }}>
                <div style={{ fontSize: '0.75rem', color: 'var(--text-muted)', marginBottom: '0.5rem' }}>
                  Consumption Order (days taken from policies in this order)
                </div>
                <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap' }}>
                  {(balance.policies || [])
                    .filter(p => p.available > 0)
                    .sort((a, b) => a.priority - b.priority)
                    .map((p, i) => (
                      <span key={p.policy_id} style={{
                        display: 'inline-flex',
                        alignItems: 'center',
                        gap: '0.375rem',
                        padding: '0.25rem 0.625rem',
                        background: 'var(--bg-primary)',
                        borderRadius: '6px',
                        fontSize: '0.75rem',
                        border: '1px solid var(--border)',
                      }}>
                        <span style={{
                          background: 'var(--accent)',
                          color: 'white',
                          padding: '0 0.25rem',
                          borderRadius: '3px',
                          fontSize: '0.625rem',
                          fontWeight: 600,
                        }}>
                          {i + 1}
                        </span>
                        {p.policy_name}
                        <span style={{ color: 'var(--text-muted)' }}>({p.available.toFixed(1)}d)</span>
                      </span>
                    ))}
                </div>
              </div>
            </div>

            {/* Policy Cards */}
            <div style={{ fontSize: '0.75rem', color: 'var(--text-muted)', marginBottom: '0.75rem', textTransform: 'uppercase', letterSpacing: '0.05em' }}>
              Policy Details (sorted by consumption priority)
            </div>
            <div className="grid grid-2">
              {(balance.policies || [])
                .sort((a, b) => a.priority - b.priority)
                .map((p, index) => (
                  <PolicyBalanceCard key={p.policy_id} policy={p} index={index} />
                ))}
            </div>
          </div>

          {/* Transactions */}
          <div className="card">
            <div className="card-header">
              <h2 className="card-title">Transaction History</h2>
            </div>
            <TransactionList transactions={transactions || []} />
          </div>

          {/* Time Off Calendar */}
          {transactions && transactions.length > 0 && (
            <TimeOffCalendar
              transactions={transactions}
              employeeId={selectedEmployee!}
              resourceType={selectedResourceType}
            />
          )}

          {/* Request Modal */}
          {showRequestModal && balance && (
            <RequestModal
              employeeId={selectedEmployee!}
              balance={balance}
              resourceType={selectedResourceType}
              onClose={() => setShowRequestModal(false)}
              onSuccess={() => {
                queryClient.invalidateQueries({ queryKey: ['balance'] });
                queryClient.invalidateQueries({ queryKey: ['transactions'] });
                setShowRequestModal(false);
              }}
            />
          )}
        </>
      ) : balance && balance.policies && balance.policies.length === 0 ? (
        <div className="card">
          <div className="empty-state" style={{ padding: '2rem' }}>
            <AlertCircle className="empty-state-icon" size={48} />
            <h2>No Policies Available</h2>
            <p>No policies are assigned for {RESOURCE_TYPES.find(rt => rt.id === selectedResourceType)?.label || 'this resource type'}.</p>
          </div>
        </div>
      ) : (
        <div className="card">
          <div className="empty-state" style={{ padding: '2rem' }}>
            <AlertCircle className="empty-state-icon" size={48} />
            <h2>No Balance Data</h2>
            <p>Unable to load balance information. Please try again.</p>
          </div>
        </div>
      )}

      {/* Debug Panel */}
      <div style={{ marginTop: '2rem' }}>
        <button 
          onClick={() => setShowDebug(!showDebug)}
          style={{
            background: 'var(--bg-tertiary)',
            border: '1px solid var(--border)',
            borderRadius: '6px',
            padding: '0.5rem 1rem',
            cursor: 'pointer',
            fontSize: '0.75rem',
            color: 'var(--text-muted)',
            display: 'flex',
            alignItems: 'center',
            gap: '0.5rem',
          }}
        >
          <Info size={14} />
          {showDebug ? 'Hide' : 'Show'} Debug Info
        </button>
        
        {showDebug && (
          <div style={{ 
            marginTop: '1rem', 
            background: '#1e1e1e', 
            borderRadius: '8px', 
            padding: '1rem',
            overflow: 'auto',
            maxHeight: '500px',
          }}>
            <div style={{ marginBottom: '1rem' }}>
              <h4 style={{ color: '#9cdcfe', marginBottom: '0.5rem', fontSize: '0.875rem' }}>Policies ({policies?.length || 0})</h4>
              <pre style={{ 
                color: '#d4d4d4', 
                fontSize: '0.75rem', 
                margin: 0,
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-all',
              }}>
                {JSON.stringify(policies, null, 2)}
              </pre>
            </div>
            
            <div style={{ marginBottom: '1rem' }}>
              <h4 style={{ color: '#9cdcfe', marginBottom: '0.5rem', fontSize: '0.875rem' }}>Assignments ({assignments?.length || 0})</h4>
              <pre style={{ 
                color: '#d4d4d4', 
                fontSize: '0.75rem', 
                margin: 0,
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-all',
              }}>
                {JSON.stringify(assignments, null, 2)}
              </pre>
            </div>
            
            <div style={{ marginBottom: '1rem' }}>
              <h4 style={{ color: '#9cdcfe', marginBottom: '0.5rem', fontSize: '0.875rem' }}>Balance</h4>
              <pre style={{ 
                color: '#d4d4d4', 
                fontSize: '0.75rem', 
                margin: 0,
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-all',
              }}>
                {JSON.stringify(balance, null, 2)}
              </pre>
            </div>
            
            <div>
              <h4 style={{ color: '#9cdcfe', marginBottom: '0.5rem', fontSize: '0.875rem' }}>Current Scenario</h4>
              <pre style={{ 
                color: '#d4d4d4', 
                fontSize: '0.75rem', 
                margin: 0,
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-all',
              }}>
                {JSON.stringify(currentScenario, null, 2)}
              </pre>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

function PolicyBalanceCard({ policy, index }: { policy: Balance['policies'][0]; index: number }) {
  const percentage = policy.total_entitlement > 0
    ? Math.min(100, (policy.consumed / policy.total_entitlement) * 100)
    : 0;

  const progressColor = percentage > 80 ? 'var(--danger)' : percentage > 50 ? 'var(--warning)' : 'var(--success)';

  // Priority colors for visual distinction
  const priorityColors = ['var(--success)', 'var(--info)', 'var(--accent)', 'var(--warning)', 'var(--text-muted)'];
  const priorityColor = priorityColors[Math.min(index, priorityColors.length - 1)];

  return (
    <div className="balance-card" style={{ borderLeft: `3px solid ${priorityColor}` }}>
      {/* Priority Badge */}
      <div style={{ 
        display: 'flex', 
        alignItems: 'center', 
        gap: '0.5rem', 
        marginBottom: '0.5rem',
        fontSize: '0.6875rem',
        textTransform: 'uppercase',
        letterSpacing: '0.05em',
        color: priorityColor,
        fontWeight: 600,
      }}>
        <span style={{
          background: priorityColor,
          color: 'white',
          padding: '0.125rem 0.375rem',
          borderRadius: '4px',
          fontSize: '0.625rem',
        }}>
          #{policy.priority}
        </span>
        <span>Consumption Order</span>
      </div>

      <div className="balance-card-header">
        <div>
          <span className="balance-card-title" style={{ fontSize: '1rem' }}>{policy.policy_name}</span>
        </div>
        {policy.requires_approval && (
          <span className="badge badge-warning">Approval</span>
        )}
      </div>

      {/* Period Info */}
      <div style={{ 
        fontSize: '0.75rem', 
        color: 'var(--text-muted)', 
        marginBottom: '0.75rem',
        display: 'flex',
        alignItems: 'center',
        gap: '0.5rem',
      }}>
        <span>📅 Calendar Year 2025</span>
        <span style={{ color: 'var(--border)' }}>•</span>
        <span>{policy.consumption_mode === 'consume_up_to_accrued' ? 'Earn-then-use' : 'Full year available'}</span>
      </div>

      <div className="balance-card-value">
        {policy.available.toFixed(1)}
        <span className="balance-card-unit">days available</span>
        {policy.consumption_mode === 'consume_ahead' && policy.accrued_to_date < policy.total_entitlement && (
          <div style={{ 
            fontSize: '0.75rem', 
            color: 'var(--text-muted)', 
            marginTop: '0.25rem',
            fontWeight: 400
          }}>
            ({policy.accrued_to_date.toFixed(1)} days accrued so far)
          </div>
        )}
      </div>

      <div className="progress-bar" style={{ marginTop: '0.75rem' }}>
        <div
          className="progress-fill"
          style={{ width: `${percentage}%`, background: progressColor }}
        />
      </div>

      {/* Detailed breakdown */}
      <div style={{ 
        marginTop: '0.75rem', 
        display: 'grid', 
        gridTemplateColumns: policy.consumption_mode === 'consume_ahead' ? 'repeat(4, 1fr)' : 'repeat(3, 1fr)', 
        gap: '0.5rem',
        fontSize: '0.75rem',
      }}>
        <div style={{ textAlign: 'center', padding: '0.5rem', background: 'var(--bg-secondary)', borderRadius: '6px' }}>
          <div style={{ color: 'var(--text-muted)', marginBottom: '0.25rem' }}>Entitlement</div>
          <div style={{ fontWeight: 600 }}>{policy.total_entitlement.toFixed(1)}</div>
          <div style={{ fontSize: '0.625rem', color: 'var(--text-light)', marginTop: '0.125rem' }}>Total for year</div>
        </div>
        {policy.consumption_mode === 'consume_ahead' && (
          <div style={{ textAlign: 'center', padding: '0.5rem', background: 'var(--bg-secondary)', borderRadius: '6px', border: '1px solid var(--border-light)' }}>
            <div style={{ color: 'var(--text-muted)', marginBottom: '0.25rem' }}>Accrued</div>
            <div style={{ fontWeight: 600, color: 'var(--info)' }}>{policy.accrued_to_date.toFixed(1)}</div>
            <div style={{ fontSize: '0.625rem', color: 'var(--text-light)', marginTop: '0.125rem' }}>Earned so far</div>
          </div>
        )}
        <div style={{ textAlign: 'center', padding: '0.5rem', background: 'var(--bg-secondary)', borderRadius: '6px' }}>
          <div style={{ color: 'var(--text-muted)', marginBottom: '0.25rem' }}>Used</div>
          <div style={{ fontWeight: 600, color: 'var(--danger)' }}>{policy.consumed.toFixed(1)}</div>
        </div>
        <div style={{ textAlign: 'center', padding: '0.5rem', background: 'var(--bg-secondary)', borderRadius: '6px' }}>
          <div style={{ color: 'var(--text-muted)', marginBottom: '0.25rem' }}>Pending</div>
          <div style={{ fontWeight: 600, color: 'var(--warning)' }}>{policy.pending.toFixed(1)}</div>
        </div>
      </div>
      
      {policy.consumption_mode === 'consume_ahead' && (
        <div style={{ 
          marginTop: '0.75rem', 
          padding: '0.5rem', 
          background: 'rgba(59, 130, 246, 0.1)', 
          borderRadius: '6px',
          fontSize: '0.75rem',
          color: 'var(--info)',
        }}>
          💡 <strong>Consume Ahead:</strong> You can use all {policy.total_entitlement.toFixed(1)} days now, even though only {policy.accrued_to_date.toFixed(1)} days have been earned so far.
        </div>
      )}

      {policy.consumption_mode === 'consume_up_to_accrued' && (
        <div style={{ 
          marginTop: '0.75rem', 
          padding: '0.5rem', 
          background: 'rgba(245, 158, 11, 0.1)', 
          borderRadius: '6px',
          fontSize: '0.75rem',
          color: 'var(--warning)',
        }}>
          ⚠️ Only {policy.accrued_to_date.toFixed(1)} days accrued so far (earn-then-use policy)
        </div>
      )}
    </div>
  );
}

function TransactionList({ transactions }: { transactions: Transaction[] }) {
  if (!transactions.length) {
    return (
      <div className="empty-state" style={{ padding: '2rem' }}>
        <Clock className="empty-state-icon" size={32} />
        <p>No transactions yet</p>
      </div>
    );
  }

  const typeColors: Record<string, string> = {
    accrual: 'tx-accrual',
    consumption: 'tx-consumption',
    pending: 'tx-pending',
    reconciliation: 'tx-reconciliation',
    adjustment: 'tx-adjustment',
    reversal: 'tx-reversal',
  };

  return (
    <table className="table">
      <thead>
        <tr>
          <th>Date</th>
          <th>Type</th>
          <th>Amount</th>
          <th>Balance</th>
          <th>Policy</th>
          <th>Reason</th>
        </tr>
      </thead>
      <tbody>
        {transactions.slice(0, 20).map(tx => {
          const isPolicyChange = tx.type === 'reconciliation' && 
            (tx.reason?.toLowerCase().includes('policy change') || tx.reason?.toLowerCase().includes('policy change'));
          
          return (
            <tr key={tx.id} style={isPolicyChange ? { backgroundColor: 'var(--bg-secondary)' } : {}}>
              <td>{format(parseISO(tx.effective_at), 'MMM d, yyyy')}</td>
              <td>
                <span className={typeColors[tx.type] || ''}>
                  {tx.type.charAt(0).toUpperCase() + tx.type.slice(1)}
                </span>
              </td>
              <td>
                <span style={{ color: tx.delta >= 0 ? 'var(--success)' : 'var(--danger)' }}>
                  {tx.delta >= 0 ? '+' : ''}{tx.delta.toFixed(2)} {tx.unit}
                </span>
              </td>
              <td>
                {tx.balance !== undefined ? (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: '0.25rem' }}>
                    {isPolicyChange && tx.balance_after !== undefined ? (
                      <>
                        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                          <span style={{ 
                            fontWeight: 600, 
                            color: 'var(--text-muted)',
                            fontSize: '0.875rem'
                          }}>
                            Before:
                          </span>
                          <span style={{ fontWeight: 600, color: 'var(--warning)' }}>
                            {tx.balance.toFixed(2)} {tx.unit}
                          </span>
                        </div>
                        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                          <span style={{ 
                            fontWeight: 600, 
                            color: 'var(--text-muted)',
                            fontSize: '0.875rem'
                          }}>
                            After:
                          </span>
                          <span style={{ fontWeight: 600, color: 'var(--success)' }}>
                            {tx.balance_after.toFixed(2)} {tx.unit}
                          </span>
                        </div>
                      </>
                    ) : (
                      <span style={{ fontWeight: 500 }}>
                        {tx.balance.toFixed(2)} {tx.unit}
                      </span>
                    )}
                  </div>
                ) : (
                  <span style={{ color: 'var(--text-muted)' }}>-</span>
                )}
              </td>
              <td style={{ color: 'var(--text-muted)' }}>{tx.policy_id}</td>
              <td style={{ color: 'var(--text-muted)' }}>{tx.reason || '-'}</td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

function RequestModal({
  employeeId,
  balance,
  resourceType,
  onClose,
  onSuccess,
}: {
  employeeId: string;
  balance: Balance;
  resourceType: string;
  onClose: () => void;
  onSuccess: () => void;
}) {
  const [selectedDates, setSelectedDates] = useState<Date[]>([]);
  const [showDatePicker, setShowDatePicker] = useState(true);
  const [reason, setReason] = useState('');
  const [error, setError] = useState<string | null>(null);

  const resourceLabel = RESOURCE_TYPES.find(rt => rt.id === resourceType)?.label || resourceType;

  const mutation = useMutation({
    mutationFn: (days: string[]) => submitRequest(employeeId, {
      resource_type: resourceType,
      days,
      reason,
    }),
    onSuccess: (response) => {
      if (response.validation_error) {
        setError(response.validation_error);
      } else {
        onSuccess();
      }
    },
    onError: (err: Error) => setError(err.message),
  });

  const handleDateSelect = (dates: Date[]) => {
    console.log('[RequestModal] handleDateSelect called with dates:', dates);
    if (!dates || dates.length === 0) {
      console.warn('[RequestModal] No dates selected');
      return;
    }
    console.log('[RequestModal] Setting selectedDates, balance:', balance);
    setSelectedDates(dates);
    setShowDatePicker(false);
  };

  const handleSubmit = () => {
    if (selectedDates.length === 0) {
      setError('Please select at least one day');
      return;
    }
    setError(null);
    const days = selectedDates.map(d => format(d, 'yyyy-MM-dd'));
    mutation.mutate(days);
  };

  // Calculate distribution preview - MUST be called before any conditional returns (Rules of Hooks)
  const distributionPreview = useMemo(() => {
    console.log('[RequestModal] Calculating distribution preview, balance:', balance, 'selectedDates.length:', selectedDates.length);
    if (!balance || !balance.policies || balance.policies.length === 0) {
      console.warn('[RequestModal] Invalid balance in distributionPreview');
      return { allocations: [], remaining: selectedDates.length || 0 };
    }
    if (!selectedDates || selectedDates.length === 0) {
      return { allocations: [], remaining: 0 };
    }
    try {
      const sortedPolicies = [...balance.policies].sort((a, b) => a.priority - b.priority);
      let remaining = selectedDates.length;
      const allocations: Array<{ policy: typeof balance.policies[0]; amount: number }> = [];

      for (const policy of sortedPolicies) {
        if (remaining <= 0) break;
        const available = policy.available;
        if (available <= 0) continue;

        const toTake = Math.min(remaining, available);
        allocations.push({ policy, amount: toTake });
        remaining -= toTake;
      }

      console.log('[RequestModal] Distribution preview calculated:', allocations);
      return { allocations, remaining };
    } catch (error) {
      console.error('[RequestModal] Error calculating distribution preview:', error);
      return { allocations: [], remaining: selectedDates.length || 0 };
    }
  }, [balance, selectedDates]);

  // Show date picker first
  if (showDatePicker) {
    console.log('[RequestModal] Showing date picker, balance:', balance);
    if (!balance || !balance.policies || balance.policies.length === 0) {
      console.error('[RequestModal] Invalid balance:', balance);
      return (
        <div className="modal-overlay" onClick={onClose}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <h2 className="modal-title">Error</h2>
            <p>Unable to load balance information. Please try again.</p>
            <div className="modal-actions">
              <button className="btn btn-secondary" onClick={onClose}>Close</button>
            </div>
          </div>
        </div>
      );
    }
    return (
      <DateRangePicker
        onSelect={handleDateSelect}
        onClose={onClose}
        minDate={new Date()}
        availableDays={Math.floor(balance.total_available)}
      />
    );
  }

  // Safety check - if no dates selected, show picker
  if (!selectedDates || selectedDates.length === 0) {
    console.log('[RequestModal] No dates selected, showing picker');
    if (!balance || !balance.policies || balance.policies.length === 0) {
      console.error('[RequestModal] Invalid balance when no dates:', balance);
      return (
        <div className="modal-overlay" onClick={onClose}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <h2 className="modal-title">Error</h2>
            <p>Unable to load balance information. Please try again.</p>
            <div className="modal-actions">
              <button className="btn btn-secondary" onClick={onClose}>Close</button>
            </div>
          </div>
        </div>
      );
    }
    return (
      <DateRangePicker
        onSelect={handleDateSelect}
        onClose={onClose}
        minDate={new Date()}
        availableDays={Math.floor(balance.total_available)}
      />
    );
  }

  // Safety check - if balance is invalid, show error
  console.log('[RequestModal] Rendering confirmation screen, selectedDates:', selectedDates.length, 'balance:', balance);
  if (!balance || !balance.policies || balance.policies.length === 0) {
    console.error('[RequestModal] Invalid balance in confirmation screen:', balance);
    return (
      <div className="modal-overlay" onClick={onClose}>
        <div className="modal" onClick={(e) => e.stopPropagation()}>
          <h2 className="modal-title">Error</h2>
          <p>Unable to load balance information. Please try again.</p>
          <div className="modal-actions">
            <button className="btn btn-secondary" onClick={onClose}>Close</button>
          </div>
        </div>
      </div>
    );
  }

  // Confirmation screen
  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()} style={{ maxWidth: '550px' }}>
        <h2 className="modal-title">Confirm {resourceLabel} Request</h2>

        {/* Selected Dates */}
        <div className="card" style={{ padding: '1rem', marginBottom: '1rem' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.75rem' }}>
            <span style={{ color: 'var(--text-muted)', fontSize: '0.75rem', textTransform: 'uppercase', letterSpacing: '0.05em' }}>Selected dates</span>
            <button 
              className="btn btn-secondary" 
              style={{ padding: '0.25rem 0.5rem', fontSize: '0.75rem' }}
              onClick={() => setShowDatePicker(true)}
            >
              Change
            </button>
          </div>
          <div style={{ fontSize: '1.25rem', fontWeight: 600 }}>
            {selectedDates.length > 0 && (
              <>
                {format(selectedDates[0], 'MMM d')} - {format(selectedDates[selectedDates.length - 1], 'MMM d, yyyy')}
              </>
            )}
          </div>
          <div style={{ marginTop: '0.5rem', fontSize: '0.875rem', color: 'var(--text-muted)' }}>
            {selectedDates.length} workdays
          </div>
        </div>

        {/* Distribution Preview */}
        <div className="card" style={{ padding: '1rem', marginBottom: '1rem', background: 'var(--bg-dark)' }}>
          <div style={{ 
            fontSize: '0.75rem', 
            textTransform: 'uppercase', 
            letterSpacing: '0.05em', 
            color: 'var(--text-muted)', 
            marginBottom: '0.75rem' 
          }}>
            How days will be consumed (in order)
          </div>

          {distributionPreview.allocations.map((alloc, index) => (
            <div 
              key={alloc.policy.policy_id}
              style={{ 
                display: 'flex', 
                alignItems: 'center', 
                gap: '0.75rem',
                padding: '0.75rem',
                background: 'var(--bg-card)',
                borderRadius: '8px',
                marginBottom: index < distributionPreview.allocations.length - 1 ? '0.5rem' : 0,
                border: '1px solid var(--border)',
              }}
            >
              {/* Order number */}
              <div style={{
                width: '24px',
                height: '24px',
                borderRadius: '50%',
                background: 'var(--accent)',
                color: 'white',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                fontSize: '0.75rem',
                fontWeight: 600,
                flexShrink: 0,
              }}>
                {index + 1}
              </div>

              {/* Policy info */}
              <div style={{ flex: 1 }}>
                <div style={{ fontWeight: 600, fontSize: '0.875rem' }}>
                  {alloc.policy.policy_name}
                </div>
                <div style={{ fontSize: '0.75rem', color: 'var(--text-muted)' }}>
                  Priority #{alloc.policy.priority} • {alloc.policy.available.toFixed(1)} days available
                </div>
              </div>

              {/* Amount being taken */}
              <div style={{ 
                textAlign: 'right',
                padding: '0.375rem 0.75rem',
                background: 'rgba(239, 68, 68, 0.1)',
                borderRadius: '6px',
              }}>
                <div style={{ fontSize: '1rem', fontWeight: 700, color: 'var(--danger)' }}>
                  -{alloc.amount}
                </div>
                <div style={{ fontSize: '0.625rem', color: 'var(--text-muted)' }}>days</div>
              </div>
            </div>
          ))}

          {/* Insufficient balance warning */}
          {distributionPreview.remaining > 0 && (
            <div style={{
              marginTop: '0.75rem',
              padding: '0.75rem',
              background: 'rgba(239, 68, 68, 0.1)',
              borderRadius: '8px',
              display: 'flex',
              alignItems: 'center',
              gap: '0.5rem',
              color: 'var(--danger)',
              fontSize: '0.875rem',
            }}>
              <AlertCircle size={18} />
              <span>
                <strong>Insufficient balance!</strong> Short by {distributionPreview.remaining} days
              </span>
            </div>
          )}

          {/* Summary */}
          <div style={{ 
            marginTop: '0.75rem', 
            paddingTop: '0.75rem', 
            borderTop: '1px solid var(--border)',
            display: 'flex',
            justifyContent: 'space-between',
            fontSize: '0.875rem',
          }}>
            <span style={{ color: 'var(--text-muted)' }}>Total from {distributionPreview.allocations.length} {distributionPreview.allocations.length === 1 ? 'policy' : 'policies'}</span>
            <span style={{ fontWeight: 600 }}>
              {distributionPreview.allocations.reduce((sum, a) => sum + a.amount, 0)} days
            </span>
          </div>
        </div>

        <div className="form-group">
          <label className="form-label">Reason (optional)</label>
          <input
            type="text"
            className="form-input"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            placeholder="Vacation, appointment, etc."
          />
        </div>

        {error && (
          <div style={{ marginTop: '1rem', padding: '0.75rem', background: 'rgba(239, 68, 68, 0.1)', borderRadius: '8px', display: 'flex', alignItems: 'center', gap: '0.5rem', color: 'var(--danger)' }}>
            <AlertCircle size={16} />
            {error}
          </div>
        )}

        {mutation.isSuccess && !error && (
          <div style={{ marginTop: '1rem', padding: '0.75rem', background: 'rgba(16, 185, 129, 0.1)', borderRadius: '8px', display: 'flex', alignItems: 'center', gap: '0.5rem', color: 'var(--success)' }}>
            <Check size={16} />
            Request submitted successfully!
          </div>
        )}

        <div className="modal-actions">
          <button type="button" className="btn btn-secondary" onClick={onClose}>
            Cancel
          </button>
          <button
            className="btn btn-primary"
            onClick={handleSubmit}
            disabled={mutation.isPending || selectedDates.length === 0}
          >
            {mutation.isPending ? 'Submitting...' : (
              <>
                <Check size={16} />
                Submit Request
              </>
            )}
          </button>
        </div>
      </div>
    </div>
  );
}
