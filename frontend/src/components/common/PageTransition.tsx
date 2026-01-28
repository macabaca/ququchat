import React, { useRef } from 'react';
import { CSSTransition, TransitionGroup } from 'react-transition-group';
import { useLocation } from 'react-router-dom';
import '../../styles/transitions.css';

interface PageTransitionProps {
  children: React.ReactNode;
}

// Wrapper component to handle nodeRef for strict mode compatibility
const TransitionWrapper = ({ children, ...props }: any) => {
  const nodeRef = useRef(null);

  return (
    <CSSTransition
      {...props}
      nodeRef={nodeRef}
      classNames="page-transition"
      timeout={300}
      unmountOnExit
    >
      <div ref={nodeRef} className="page-transition-container">
        {children}
      </div>
    </CSSTransition>
  );
};

const PageTransition: React.FC<PageTransitionProps> = ({ children }) => {
  const location = useLocation();

  return (
    <TransitionGroup component={null}>
      <TransitionWrapper key={location.key}>
        {/* 
          Clone the children (Routes) and pass the CURRENT location 
          so that when this component is exiting, it still renders 
          the old route content instead of the new one.
        */}
        {React.isValidElement(children) 
          ? React.cloneElement(children as React.ReactElement<any>, { location }) 
          : children}
      </TransitionWrapper>
    </TransitionGroup>
  );
};

export default PageTransition;
