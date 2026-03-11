/**
 * Shiper Frontend Logic
 * Handles project management, dashboard rendering, and build triggering.
 */

const API_BASE = './api';

// DOM Elements
const dom = {
    projectsContainer: document.getElementById('projects-container'),
    btnOpenModal: document.getElementById('btn-open-modal'),
    modal: document.getElementById('add-modal'),
    btnCloseModal: document.getElementById('btn-close-modal'),
    btnCancelModal: document.getElementById('btn-cancel-modal'),
    form: document.getElementById('add-project-form'),
    inputs: {
        name: document.getElementById('proj-name'),
        repo: document.getElementById('proj-repo'),
        branch: document.getElementById('proj-branch'),
    }
};

// State
let isPolling = false;

function init() {
    bindEvents();
    fetchProjects();
    startPolling();
}

function bindEvents() {
    // Modal controls
    dom.btnOpenModal.addEventListener('click', openModal);
    dom.btnCloseModal.addEventListener('click', closeModal);
    dom.btnCancelModal.addEventListener('click', closeModal);
    
    // Form submission
    dom.form.addEventListener('submit', handleAddProject);
    
    // Event delegation for project cards (Build buttons)
    dom.projectsContainer.addEventListener('click', (e) => {
        const btn = e.target.closest('button[data-action="build"]');
        if (btn) {
            const id = btn.dataset.id;
            triggerBuild(id);
        }
    });
}

function startPolling() {
    if (isPolling) return;
    isPolling = true;
    // Poll every 3 seconds to update statuses
    setInterval(fetchProjects, 3000);
}

// --- UI Actions ---

function openModal() {
    dom.modal.classList.remove('hidden');
    dom.inputs.name.focus();
}

function closeModal() {
    dom.modal.classList.add('hidden');
    dom.form.reset();
}

// --- API Interactions ---

async function fetchProjects() {
    try {
        const res = await fetch(`${API_BASE}/projects`);
        if (!res.ok) throw new Error(`API Error: ${res.status}`);
        const projects = await res.json();
        renderProjects(projects);
    } catch (err) {
        console.error('Failed to fetch projects:', err);
    }
}

async function handleAddProject(e) {
    e.preventDefault();
    
    const submitBtn = dom.form.querySelector('button[type="submit"]');
    const originalText = submitBtn.innerText;
    submitBtn.innerText = 'Saving...';
    submitBtn.disabled = true;

    const payload = {
        name: dom.inputs.name.value,
        repo_url: dom.inputs.repo.value,
        branch: dom.inputs.branch.value
    };

    try {
        const res = await fetch(`${API_BASE}/projects`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });

        if (!res.ok) {
            const errText = await res.text();
            throw new Error(errText || 'Failed to create project');
        }

        closeModal();
        fetchProjects();
    } catch (err) {
        alert(`Error: ${err.message}`);
    } finally {
        submitBtn.innerText = originalText;
        submitBtn.disabled = false;
    }
}

async function triggerBuild(id) {
    try {
        const res = await fetch(`${API_BASE}/projects/${id}/build`, { method: 'POST' });
        if (!res.ok) throw new Error('Failed to trigger build');
        
        // Force immediate refresh to show "building" status
        fetchProjects(); 
    } catch (err) {
        alert(`Error triggering build: ${err.message}`);
    }
}

// --- Rendering ---

function renderProjects(projects) {
    if (!projects || projects.length === 0) {
        dom.projectsContainer.innerHTML = `
            <div class="text-gray-400 text-center py-12 bg-gray-800/50 rounded-lg border border-gray-700 border-dashed animate-pulse">
                <p class="text-lg font-medium text-gray-300">No projects yet</p>
                <p class="text-sm text-gray-500">Add a project to get started</p>
            </div>`;
        return;
    }

    dom.projectsContainer.innerHTML = projects.map(p => {
        const statusColors = {
            'success': 'text-green-400 bg-green-400/10 border-green-400/20',
            'failed': 'text-red-400 bg-red-400/10 border-red-400/20',
            'building': 'text-blue-400 bg-blue-400/10 border-blue-400/20 animate-pulse',
            'pending': 'text-gray-400 bg-gray-400/10 border-gray-400/20'
        };
        const statusClass = statusColors[p.status] || statusColors['pending'];
        const isBuilding = p.status === 'building';

        return `
        <div class="bg-gray-800 border border-gray-700 rounded-lg p-5 shadow-sm hover:border-gray-600 transition-all group">
            <div class="flex justify-between items-start mb-4">
                <div class="flex items-center space-x-3">
                    <div class="p-2 bg-gray-700 rounded-md">
                        <svg class="w-6 h-6 text-blue-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 002-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10"></path></svg>
                    </div>
                    <div>
                        <h3 class="text-lg font-bold text-white tracking-tight">${p.name}</h3>
                        <div class="text-xs text-gray-400 font-mono mt-0.5">${p.repo_url} <span class="text-gray-600 mx-1">|</span> <span class="text-blue-400">${p.branch}</span></div>
                    </div>
                </div>
                <span class="px-2.5 py-0.5 rounded-full text-xs font-medium border flex items-center gap-1.5 ${statusClass} capitalize">
                    ${isBuilding ? '<svg class="animate-spin w-3 h-3" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path></svg>' : '<span class="w-1.5 h-1.5 rounded-full bg-current"></span>'}
                    ${p.status || 'pending'}
                </span>
            </div>
            
            <div class="flex justify-between items-end border-t border-gray-700/50 pt-4 mt-2">
                <div class="flex flex-col">
                    <span class="text-[10px] uppercase tracking-wider font-semibold text-gray-500 mb-0.5">Current Version</span>
                    <span class="text-sm font-mono text-gray-200">${p.version || '0.0.0'}</span>
                </div>
                
                <button 
                    data-action="build" 
                    data-id="${p.id}"
                    class="bg-gray-700 hover:bg-blue-600 hover:text-white text-gray-200 px-4 py-2 rounded-md text-sm font-medium transition-colors border border-gray-600 shadow-sm flex items-center gap-2 group-hover:border-gray-500 ${isBuilding ? 'opacity-50 cursor-not-allowed' : ''}"
                    ${isBuilding ? 'disabled' : ''}
                >
                    <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z"></path><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z"></path></svg>
                    Build Now
                </button>
            </div>
        </div>
        `;
    }).join('');
}

init();