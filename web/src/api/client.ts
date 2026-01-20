// API client for the time resource engine

const API_BASE = '/api';

// =============================================================================
// TYPES
// =============================================================================

export interface Employee {
  id: string;
  name: string;
  email: string;
  hire_date: string;
  created_at?: string;
}

export interface Policy {
  id: string;
  name: string;
  resource_type: string;
  config: PolicyConfig;
  version: number;
  created_at?: string;
}

export interface PolicyConfig {
  id: string;
  name: string;
  resource_type: string;
  unit: string;
  period_type: string;
  consumption_mode?: string;
  is_unlimited?: boolean;
  accrual?: {
    type: string;
    annual_days?: number;
    frequency?: string;
  };
  constraints?: {
    allow_negative?: boolean;
    max_balance?: number;
  };
  reconciliation_rules?: Array<{
    trigger: string;
    actions: Array<{
      type: string;
      max_carryover?: number;
    }>;
  }>;
}

export interface Assignment {
  id: string;
  entity_id: string;
  policy_id: string;
  policy_name?: string;
  effective_from: string;
  effective_to?: string;
  consumption_priority: number;
  requires_approval: boolean;
  auto_approve_up_to?: number;
}

export interface Balance {
  entity_id: string;
  resource_type: string;
  total_available: number;
  total_pending: number;
  policies: PolicyBalance[];
  as_of: string;
}

export interface PolicyBalance {
  policy_id: string;
  policy_name: string;
  priority: number;
  available: number;
  accrued_to_date: number;
  total_entitlement: number;
  consumed: number;
  pending: number;
  consumption_mode: string;
  requires_approval: boolean;
}

export interface Transaction {
  id: string;
  entity_id: string;
  policy_id: string;
  resource_type: string;
  effective_at: string;
  delta: number;
  unit: string;
  type: string;
  reference_id?: string;
  reason?: string;
  created_at?: string;
  balance?: number; // Balance at this transaction date (before)
  balance_after?: number; // Balance after this transaction (for policy changes)
}

export interface TimeOffRequest {
  entity_id: string;
  resource_type: string;
  days: string[];
  reason?: string;
}

export interface TimeOffResponse {
  request_id: string;
  status: string;
  distribution: Array<{
    policy_id: string;
    policy_name: string;
    amount: number;
  }>;
  total_days: number;
  requires_approval: boolean;
  validation_error?: string;
}

export interface RolloverResult {
  entity_id: string;
  policy_id: string;
  carried_over: number;
  expired: number;
  transactions: Transaction[];
}

export interface Scenario {
  id: string;
  name: string;
  description: string;
  category?: string; // "timeoff" or "rewards"
}

// =============================================================================
// API FUNCTIONS
// =============================================================================

async function fetchJSON<T>(url: string, options?: RequestInit): Promise<T> {
  const res = await fetch(API_BASE + url, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...options?.headers,
    },
  });

  if (!res.ok) {
    const errorData = await res.json().catch(() => ({ error: res.statusText }));
    const errorMessage = errorData.error || errorData.message || 'Request failed';
    const errorDetails = errorData.details ? `: ${errorData.details}` : '';
    throw new Error(errorMessage + errorDetails);
  }

  return res.json();
}

// Employees
export const getEmployees = () => fetchJSON<Employee[]>('/employees');
export const getEmployee = (id: string) => fetchJSON<Employee>(`/employees/${id}`);
export const createEmployee = (data: Omit<Employee, 'created_at'>) =>
  fetchJSON<Employee>('/employees', { method: 'POST', body: JSON.stringify(data) });

// Balance
export const getBalance = (employeeId: string, resourceType = 'pto') =>
  fetchJSON<Balance>(`/employees/${employeeId}/balance?resource_type=${resourceType}`);

// Transactions
export const getTransactions = (employeeId: string, policyId?: string) => {
  let url = `/employees/${employeeId}/transactions`;
  if (policyId) url += `?policy_id=${policyId}`;
  return fetchJSON<Transaction[]>(url);
};

export interface CancelResponse {
  status: string;
  transaction_id: string;
  reversal_id: string;
  date: string;
  amount: string;
}

export const cancelTransaction = (transactionId: string) =>
  fetchJSON<CancelResponse>(`/transactions/${transactionId}`, { method: 'DELETE' });

// Requests
export const submitRequest = (employeeId: string, request: Omit<TimeOffRequest, 'entity_id'>) =>
  fetchJSON<TimeOffResponse>(`/employees/${employeeId}/requests`, {
    method: 'POST',
    body: JSON.stringify({ ...request, entity_id: employeeId }),
  });

// Assignments
export const getAssignments = (employeeId: string) =>
  fetchJSON<Assignment[]>(`/employees/${employeeId}/assignments`);
export const createAssignment = (data: Omit<Assignment, 'id' | 'policy_name'>) =>
  fetchJSON<Assignment>('/admin/assignments', { method: 'POST', body: JSON.stringify(data) });

// Policies
export const getPolicies = () => fetchJSON<Policy[]>('/policies');
export const getPolicy = (id: string) => fetchJSON<Policy>(`/policies/${id}`);
export const createPolicy = (config: PolicyConfig) =>
  fetchJSON<Policy>('/policies', { method: 'POST', body: JSON.stringify({ config }) });

// Admin
export const triggerRollover = (data: { entity_id?: string; policy_id?: string; period_end: string }) =>
  fetchJSON<RolloverResult[]>('/admin/rollover', { method: 'POST', body: JSON.stringify(data) });
export const createAdjustment = (data: { entity_id: string; policy_id: string; delta: number; reason: string }) =>
  fetchJSON<Transaction>('/admin/adjustments', { method: 'POST', body: JSON.stringify(data) });

// Scenarios
export const getScenarios = () => fetchJSON<Scenario[]>('/scenarios');
export const getCurrentScenario = () => fetchJSON<Scenario | null>('/scenarios/current');
export const loadScenario = (scenarioId: string) =>
  fetchJSON<{ status: string; scenario: string }>('/scenarios/load', {
    method: 'POST',
    body: JSON.stringify({ scenario_id: scenarioId }),
  });
export const resetDatabase = () =>
  fetchJSON<{ status: string }>('/scenarios/reset', { method: 'POST' });

// =============================================================================
// HOLIDAYS
// =============================================================================

export interface Holiday {
  id: string;
  company_id: string;
  date: string;
  name: string;
  recurring: boolean;
}

export const getHolidays = (companyId = '') =>
  fetchJSON<{ holidays: Holiday[] }>(`/holidays?company_id=${companyId}`);

export const createHoliday = (data: Omit<Holiday, 'id'>) =>
  fetchJSON<{ status: string; holiday: string }>('/holidays', {
    method: 'POST',
    body: JSON.stringify(data),
  });

export const deleteHoliday = (id: string) =>
  fetchJSON<{ status: string }>(`/holidays/${id}`, { method: 'DELETE' });

export const addDefaultHolidays = (companyId = '') =>
  fetchJSON<{ status: string; count: number }>('/holidays/defaults', {
    method: 'POST',
    body: JSON.stringify({ company_id: companyId }),
  });

// =============================================================================
// APPROVAL WORKFLOW
// =============================================================================

export interface PendingRequest {
  id: string;
  entity_id: string;
  employee_name: string;
  resource_type: string;
  effective_at: string;
  amount: number;
  unit: string;
  reason: string;
  created_at: string;
}

export const getPendingRequests = () =>
  fetchJSON<{ requests: PendingRequest[] }>('/requests/pending');

export const approveRequest = (id: string, approverId = 'admin') =>
  fetchJSON<{ status: string; approved_by: string }>(`/requests/${id}/approve`, {
    method: 'POST',
    body: JSON.stringify({ approver_id: approverId }),
  });

export const rejectRequest = (id: string, reason: string, rejecterId = 'admin') =>
  fetchJSON<{ status: string; rejected_by: string; reason: string }>(`/requests/${id}/reject`, {
    method: 'POST',
    body: JSON.stringify({ rejecter_id: rejecterId, reason }),
  });

// =============================================================================
// RECONCILIATION
// =============================================================================

export interface ReconciliationRun {
  id: string;
  policy_id: string;
  entity_id: string;
  period_start: string;
  period_end: string;
  status: string;
  carried_over: number;
  expired: number;
  error?: string;
  completed_at?: string;
}

export const getReconciliationRuns = (status = '') =>
  fetchJSON<{ runs: ReconciliationRun[] }>(`/reconciliation/runs?status=${status}`);

export const triggerReconciliation = (data: { entity_id?: string; policy_id?: string; period_end: string }) =>
  fetchJSON<RolloverResult[]>('/reconciliation/process', {
    method: 'POST',
    body: JSON.stringify(data),
  });
